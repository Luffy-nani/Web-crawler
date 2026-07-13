package frontier

import (
	"net/url"
	"strings"
	"sync"
	"time"
)

type Frontier struct {
	mu      sync.Mutex
	cond    *sync.Cond // lets Next() sleep instead of polling, wakes on Add()/Done()
	seen    map[string]struct{}
	hosts   map[string]*HostQueue
	pending int // URLs handed out by Next() but not yet reported Done()
}

type HostQueue struct {
	URLS       []string
	LastFetch  time.Time
	CrawlDelay time.Duration
}

func New() *Frontier {
	f := &Frontier{
		seen:  make(map[string]struct{}),
		hosts: make(map[string]*HostQueue),
	}
	f.cond = sync.NewCond(&f.mu) // cond shares the same lock as f.mu
	return f
}

// Add normalizes and queues a URL if it hasn't been seen before.
// Called by main (for seed URLs) and by workers (for discovered links).
func (f *Frontier) Add(rawURL string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}

	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	if (u.Scheme == "http" && u.Port() == "80") ||
		(u.Scheme == "https" && u.Port() == "443") {
		u.Host = u.Hostname()
	}

	u.Fragment = ""

	normalized := u.String()

	f.mu.Lock()
	defer f.mu.Unlock()

	if _, exists := f.seen[normalized]; exists {
		return
	}
	f.seen[normalized] = struct{}{}

	hq, exists := f.hosts[u.Host]
	if !exists {
		hq = &HostQueue{CrawlDelay: time.Second}
		f.hosts[u.Host] = hq
	}
	hq.URLS = append(hq.URLS, normalized)

	// New work just arrived - wake a worker that might be sleeping in Wait().
	f.cond.Broadcast()
}

// Next returns the next URL to crawl. ok is false only when the frontier
// is truly finished: nothing queued, and nothing in-flight that could
// still produce more URLs.
func (f *Frontier) Next() (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for {
		// 1. Look for a URL whose host is past its crawl delay.
		for _, host := range f.hosts {
			if len(host.URLS) == 0 {
				continue
			}
			if time.Since(host.LastFetch) >= host.CrawlDelay {
				u := host.URLS[0]
				host.URLS = host.URLS[1:]
				host.LastFetch = time.Now()
				f.pending++
				return u, true
			}
		}

		// 2. Nothing ready to hand out right now. Are we truly done?
		if f.pending == 0 && !f.hasQueuedWorkLocked() {
			return "", false
		}

		// 3. Not done. If there's genuinely nothing queued at all
		// (just in-flight workers who might Add() more), sleep
		// efficiently until something changes.
		if !f.hasQueuedWorkLocked() {
			f.cond.Wait() // unlocks f.mu while sleeping, relocks on wake
			continue
		}

		// 4. There IS queued work, it's just on cooldown (crawl delay).
		// Bounded wait, then re-check - Cond can't wake us for a timer.
		f.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
		f.mu.Lock()
	}
}

// Done reports that a worker finished processing the URL it was given
// (whether or not it found new links). Must be called exactly once per
// successful Next() call.
func (f *Frontier) Done() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.pending--

	// If that was the last piece of in-flight work AND nothing's queued,
	// every worker sleeping in Next() needs to wake up and see "done".
	if f.pending == 0 && !f.hasQueuedWorkLocked() {
		f.cond.Broadcast()
	}
}

// hasQueuedWorkLocked reports whether any host still has URLs waiting.
// Caller must already hold f.mu.
func (f *Frontier) hasQueuedWorkLocked() bool {
	for _, h := range f.hosts {
		if len(h.URLS) > 0 {
			return true
		}
	}
	return false
}