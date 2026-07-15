package robot

import (
	"golang.org/x/sync/singleflight"
	"webcrawler/internal/fetcher"

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
	return &Rules{}
}