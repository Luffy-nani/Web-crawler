package main

import (
	"log"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"webcrawler/internal/fetcher"
	"webcrawler/internal/frontier"
	"webcrawler/internal/parser"
	"webcrawler/internal/robots"
	"webcrawler/internal/store"
)

const maxPages = 50
const numWorkers = 5
const crawlInterval = 2 * time.Minute // how often to re-crawl the tracked seeds

func main() {
	fetch := fetcher.New()
	parse := parser.New()
	robot := robots.New(fetch)

	db, err := store.New("crawler.db")
	if err != nil {
		log.Fatalf("failed to open store: %v", err)
	}
	defer db.Close()

	seeds := []string{"https://example.com"} // sites you want tracked over time

	// Run the first cycle immediately - a Ticker's first tick only fires
	// AFTER the interval elapses, so without this line you'd wait 2 minutes
	// before anything happened at all.
	runCrawlCycle(seeds, fetch, parse, robot, db)

	ticker := time.NewTicker(crawlInterval)
	defer ticker.Stop()

	// Each tick blocks here until runCrawlCycle() returns, so cycles never
	// overlap - the next one only starts once the previous one is fully done.
	for range ticker.C {
		runCrawlCycle(seeds, fetch, parse, robot, db)
	}
}

// runCrawlCycle runs ONE full crawl pass over the given seeds, using a
// brand new Frontier (fresh dedup set) each time it's called. That's what
// makes revisiting the same URLs across cycles work correctly - dedup
// only applies WITHIN a single cycle, not across cycles.
func runCrawlCycle(seeds []string, fetch *fetcher.Fetcher, parse *parser.Parser, robot *robots.Robots, db *store.Store) {
	log.Println("=== starting new crawl cycle ===")

	f := frontier.New()
	for _, seed := range seeds {
		f.Add(seed)
	}

	var pagesCount atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(f, fetch, parse, &pagesCount, &wg, robot, db)
	}
	wg.Wait()

	log.Printf("=== crawl cycle finished, %d pages fetched ===\n", pagesCount.Load())
}

func worker(f *frontier.Frontier, fetch *fetcher.Fetcher, parse *parser.Parser, pagesCount *atomic.Int64, wg *sync.WaitGroup, robot *robots.Robots, db *store.Store) {
	defer wg.Done()

	for {
		rawurl, ok := f.Next()
		if !ok {
			return
		}

		fresult, err := fetch.Fetch(rawurl)
		if err != nil {
			log.Printf("fetch error: %v", err)
			db.Save(&store.Record{
				URL:       rawurl,
				FetchedAt: time.Now(),
				Err:       err.Error(),
			})
			f.Done()
			continue
		}

		links, err := parse.ExtractLinks(fresult.URL, fresult.Body)
		if err != nil {
			log.Printf("parse error: %v", err)
			db.Save(&store.Record{
				URL:        rawurl,
				StatusCode: fresult.StatusCode,
				FetchedAt:  time.Now(),
				Err:        err.Error(),
			})
			f.Done()
			continue
		}

		if pagesCount.Load() < maxPages {
			for _, link := range links {
				u, err := url.Parse(link)
				if err != nil {
					continue
				}

				if !robot.Allowed(u.Host, u.Path) {
					continue
				}

				if delay, ok := robot.CrawlDelay(u.Host); ok {
					f.SetCrawlDelay(u.Host, delay)
				}

				f.Add(link)
			}
		}

		db.Save(&store.Record{
			URL:         rawurl,
			StatusCode:  fresult.StatusCode,
			ContentType: fresult.ContentType,
			Body:        fresult.Body,
			FetchedAt:   time.Now(),
			Err:         "",
		})

		count := pagesCount.Add(1)
		log.Printf("[%d/%d] fetched %s (%d links found)", count, int64(maxPages), rawurl, len(links))

		f.Done()

		if count >= maxPages {
			return
		}
	}
}