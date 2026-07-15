package robot

import (
	"strconv"
	"webcrawler/internal/fetcher"

	"golang.org/x/sync/singleflight"

	"bufio"
	"strings"
	"sync"
	"time"
)

type Rules struct {
	Disallow   []string
	Allow      []string
	CrawlDelay time.Duration
}

type Robots struct {
	rules map[string]*Rules
	sg    singleflight.Group
	fetch *fetcher.Fetcher
	mu    sync.Mutex // this is for the rules map, not for the singleflight group
}

func New(fetch *fetcher.Fetcher) *Robots {
	return &Robots{
		rules: make(map[string]*Rules),
		fetch: fetch,
	}
}

func (r *Robots) GetRules(host string) (*Rules, error) {

	// Check if we already have the rules for this host
	r.mu.Lock()
	rules, ok := r.rules[host]
	r.mu.Unlock()
	if ok {
		return rules, nil
	}

	// Use singleflight to ensure only one fetch for the same host
	v, err, _ := r.sg.Do(host, func() (interface{}, error) {
		// Fetch the robots.txt file
		url := "https://" + host + "/robots.txt"
		fresult, err := r.fetch.Fetch(url)

		var parsed *Rules
		if err != nil {
			// Missing/unreachable robots.txt is NOT a violation - it just
			// means the site stated no rules, so default to allow-everything.
			parsed = &Rules{}
		} else {
			parsed = parse(fresult.Body)
		}

		// Write into the permanent cache BEFORE returning, so the next
		// GetRules() call for this host hits the fast path instead of
		// going through singleflight (and re-fetching) again.
		r.mu.Lock()
		r.rules[host] = parsed
		r.mu.Unlock()

		return parsed, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*Rules), nil
}

// parse turns robots.txt contents into Rules for the "*" user-agent group.
func parse(body []byte) *Rules {
	rules := &Rules{}
	scanner := bufio.NewScanner(strings.NewReader(string(body)))

	applies := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 2) //dont use just split cause it splits at every coln
		if len(parts) != 2 {
			continue
		} // this is a good error handling

		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])

		switch key {

		case "user-agent":
			// We only care about the wildcard group.
			applies = (value == "*")

		case "disallow":
			if applies && value != "" {
				rules.Disallow = append(rules.Disallow, value)
			}

		case "allow":
			if applies && value != "" {
				rules.Allow = append(rules.Allow, value)
			}

		case "crawl-delay":
			if !applies {
				continue
			}

			seconds, err := strconv.ParseFloat(value, 64)
			if err != nil {
				continue
			}

			rules.CrawlDelay = time.Duration(seconds * float64(time.Second))
		}
	}

	return rules
}

// Allowed reports whether path is allowed to be fetched for host.
// On error, fails open (true) - consistent with the rest of this package
// treating "no rules successfully retrieved" as "no restriction stated".
func (r *Robots) Allowed(host, path string) bool {
	rules, err := r.GetRules(host)
	if err != nil {
		return true
	}

	// Allow rules are checked first: robots.txt convention is that an
	// explicit Allow can carve out an exception within a broader Disallow.
	for _, a := range rules.Allow {
		if strings.HasPrefix(path, a) {
			return true
		}
	}

	for _, d := range rules.Disallow {
		if strings.HasPrefix(path, d) {
			return false
		}
	}

	// Nothing matched - default allow.
	return true
}

// CrawlDelay returns the site's requested delay, or false if unspecified
// (or if rules couldn't be retrieved at all).
func (r *Robots) CrawlDelay(host string) (time.Duration, bool) {
	rules, err := r.GetRules(host)
	if err != nil || rules.CrawlDelay <= 0 {
		return 0, false
	}
	return rules.CrawlDelay, true
}