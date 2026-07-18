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

func main() {
	f := frontier.New()
	fetch := fetcher.New()
	parse := parser.New()
	robot := robots.New(fetch)

	db, err := store.New("crawler.db")
	if err != nil {
		log.Fatalf("failed to open store: %v", err)
	}

	f.Add("https://example.com") // pick a real seed URL

	var pagesCount atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(f, fetch, parse, &pagesCount, &wg, robot, db)
	}
	wg.Wait()

	// Close AFTER wg.Wait() - workers might still be crawling and calling
	// db.Save() while wg.Wait() is blocking. Closing earlier could cause
	// a worker to send on a closed channel, which panics in Go.
	if err := db.Close(); err != nil {
		log.Printf("error closing store: %v", err)
	}

	log.Printf("crawl finished, %d pages fetched\n", pagesCount.Load())
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