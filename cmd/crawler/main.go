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
		u, err := url.Parse(rawurl)
		if err != nil {
			log.Printf("error parsing url: %v", err)
			f.Done()
			continue
		}
		if !robot.Allowed(u.Host, u.Path) {
			log.Printf("URL %s is disallowed by robots.txt", rawurl)
			f.Done()
			continue
		}
		if delay, ok := robot.CrawlDelay(u.Host); ok {
	f.SetCrawlDelay(u.Host, delay)
}
		fresult, err := fetch.Fetch(rawurl)
		if err != nil {
			log.Printf("fetch error: %v", err)
			f.Done() // still must report Done - Next() gave us this URL, we're finished with it
			continue
		}

		links, err := parse.ExtractLinks(fresult.URL, fresult.Body)
		if err != nil {
			log.Printf("parse error: %v", err)
			f.Done()
			continue
		}

		// Stop discovering new work once we've hit the page cap, but still
		// report Done() for the URL we just finished either way.
		if pagesCount.Load() < maxPages {
			for _, link := range links {
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
