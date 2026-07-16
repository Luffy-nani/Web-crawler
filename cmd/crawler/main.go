package main

import (
	"log"
	"net/url"
	"sync"
	"sync/atomic"

	"webcrawler/internal/fetcher"
	"webcrawler/internal/frontier"
	"webcrawler/internal/parser"
	"webcrawler/internal/robots"
)

const maxPages = 50
const numWorkers = 5

func main() {
	f := frontier.New()
	fetch := fetcher.New()
	parse := parser.New()
	robot := robots.New(fetch)

	f.Add("https://example.com") // pick a real seed URL

	var pagesCount atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(f, fetch, parse, &pagesCount, &wg, robot)
	}
	wg.Wait()

	log.Printf("crawl finished, %d pages fetched\n", pagesCount.Load())
}

func worker(f *frontier.Frontier, fetch *fetcher.Fetcher, parse *parser.Parser, pagesCount *atomic.Int64, wg *sync.WaitGroup, robot *robots.Robots) {
	defer wg.Done()

	for {
		rawurl, ok := f.Next()
		if !ok {
			return
		}

		// NOTE: no Allowed() check here anymore - by the time a URL is in
		// the frontier at all, it was already verified allowed before
		// being Add()'d (see the link loop below). Checking here would be
		// too late: Next() already stamped LastFetch for this host the
		// moment it handed the URL out, so a disallowed URL discarded here
		// would have wasted that host's crawl-delay cooldown for nothing.

		fresult, err := fetch.Fetch(rawurl)
		if err != nil {
			log.Printf("fetch error: %v", err)
			f.Done()
			continue
		}

		links, err := parse.ExtractLinks(fresult.URL, fresult.Body)
		if err != nil {
			log.Printf("parse error: %v", err)
			f.Done()
			continue
		}

		if pagesCount.Load() < maxPages {
			for _, link := range links {
				u, err := url.Parse(link)
				if err != nil {
					continue
				}

				// Check BEFORE adding to the frontier, not after - this is
				// the fix. A disallowed URL should never occupy a spot in
				// a host's queue or trigger that host's crawl-delay cooldown.
				if !robot.Allowed(u.Host, u.Path) {
					continue
				}

				if delay, ok := robot.CrawlDelay(u.Host); ok {
					f.SetCrawlDelay(u.Host, delay)
				}

				f.Add(link)
			}
		}

		count := pagesCount.Add(1)
		log.Printf("[%d/%d] fetched %s (%d links found)", count, int64(maxPages), rawurl, len(links))

		f.Done()

		if count >= maxPages {
			return
		}
	}
}
