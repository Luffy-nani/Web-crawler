package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"log"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"webcrawler/internal/analyzer"
	"webcrawler/internal/fetcher"
	"webcrawler/internal/frontier"
	"webcrawler/internal/parser"
	"webcrawler/internal/robots"
	"webcrawler/internal/store"
)

const maxPages = 50
const numWorkers = 5
const crawlInterval = 2 * time.Minute

func main() {
	fetch := fetcher.New()
	parse := parser.New()
	robot := robots.New(fetch)
	analyze := analyzer.New()

	db, err := store.New("crawler.db")
	if err != nil {
		log.Fatalf("failed to open store: %v", err)
	}
	defer db.Close()

	seeds := []string{"https://example.com"}

	runCrawlCycle(seeds, fetch, parse, robot, analyze, db)

	ticker := time.NewTicker(crawlInterval)
	defer ticker.Stop()

	for range ticker.C {
		runCrawlCycle(seeds, fetch, parse, robot, analyze, db)
	}
}

func runCrawlCycle(seeds []string, fetch *fetcher.Fetcher, parse *parser.Parser, robot *robots.Robots, analyze *analyzer.Analyzer, db *store.Store) {
	log.Println("=== starting new crawl cycle ===")

	f := frontier.New()
	for _, seed := range seeds {
		f.Add(seed)
	}

	var pagesCount atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(f, fetch, parse, &pagesCount, &wg, robot, analyze, db)
	}
	wg.Wait()

	log.Printf("=== crawl cycle finished, %d pages fetched ===\n", pagesCount.Load())
}

func worker(f *frontier.Frontier, fetch *fetcher.Fetcher, parse *parser.Parser, pagesCount *atomic.Int64, wg *sync.WaitGroup, robot *robots.Robots, analyze *analyzer.Analyzer, db *store.Store) {
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

		newText, err := parse.ExtractText(fresult.Body)
		if err != nil {
			log.Printf("extract text error: %v", err)
			newText = "" // don't fail the whole crawl over this - just skip comparison below
		}

		// Check history BEFORE saving this crawl's record, so we're
		// comparing against the PREVIOUS crawl, not this one.
		prev, found, err := db.GetLatestSnapshot(rawurl)
		if err != nil {
			log.Printf("history lookup error: %v", err)
		} else if !found {
			log.Printf("first time seeing %s - nothing to compare yet", rawurl)
		} else if newText != "" && prev.ExtractedText != "" {
			result, err := analyze.CompareSnapshots(prev.ExtractedText, newText)
			if err != nil {
				log.Printf("analyzer error for %s: %v", rawurl, err)
			} else if result.Changed {
				log.Printf(">>> CHANGE DETECTED on %s: %s", rawurl, result.Summary)
			}
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

		compressedBody, err := gzipCompress(fresult.Body)
		if err != nil {
			log.Printf("compress error: %v", err)
			compressedBody = nil // store without body rather than fail the whole save
		}

		db.Save(&store.Record{
			URL:           rawurl,
			StatusCode:    fresult.StatusCode,
			ContentType:   fresult.ContentType,
			Body:          compressedBody,
			ExtractedText: newText,
			FetchedAt:     time.Now(),
			Err:           "",
		})

		count := pagesCount.Add(1)
		log.Printf("[%d/%d] fetched %s (%d links found)", count, int64(maxPages), rawurl, len(links))

		f.Done()

		if count >= maxPages {
			return
		}
	}
}

// gzipCompress compresses raw bytes for storage - HTML compresses very
func gzipCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
// not called anywhere in this codebase, but could be useful for future retrieval of stored pages
func gzipDecompress(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	return io.ReadAll(gz)
}