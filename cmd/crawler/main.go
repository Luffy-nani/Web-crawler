package main

import (
	"log"
	"sync"
	"sync/atomic"

	"webcrawler/internal/fetcher"
	"webcrawler/internal/frontier"
	"webcrawler/internal/parser"
)

const maxPages = 50
const numWorkers = 5

func main() {
	f := frontier.New()
	fetch := fetcher.New()
	parse := parser.New()

	f.Add("https://example.com") // pick a real seed URL

	var pagesCount atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(f, fetch, parse, &pagesCount, &wg)
	}
	wg.Wait()

	log.Printf("crawl finished, %d pages fetched\n", pagesCount.Load())
}

func worker(f *frontier.Frontier, fetch *fetcher.Fetcher, parse *parser.Parser, pagesCount *atomic.Int64, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		url, ok := f.Next()
		if !ok {
			return
		}

		fresult, err := fetch.Fetch(url)
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
		log.Printf("[%d/%d] fetched %s (%d links found)", count, int64(maxPages), url, len(links))

		f.Done()

		if count >= maxPages {
			return
		}
	}
}