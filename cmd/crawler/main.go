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
	"webcrawler/internal/embedder"
	"webcrawler/internal/fetcher"
	"webcrawler/internal/frontier"
	"webcrawler/internal/parser"
	"webcrawler/internal/robots"
	"webcrawler/internal/store"
)

const maxPages = 50
const numWorkers = 5
const crawlInterval = 2 * time.Minute
const chunkSize = 200
const chunkOverlap = 40

func main() {
	fetch := fetcher.New()
	parse := parser.New()
	robot := robots.New(fetch)
	analyze := analyzer.New()

	embed, err := embedder.New()
	if err != nil {
		log.Fatalf("failed to init embedder: %v", err)
	}

	db, err := store.New("crawler.db")
	if err != nil {
		log.Fatalf("failed to open store: %v", err)
	}
	defer db.Close()

	seeds := []string{"https://example.com"}

	runCrawlCycle(seeds, fetch, parse, robot, analyze, embed, db)

	ticker := time.NewTicker(crawlInterval)
	defer ticker.Stop()

	for range ticker.C {
		runCrawlCycle(seeds, fetch, parse, robot, analyze, embed, db)
	}
}

func runCrawlCycle(seeds []string, fetch *fetcher.Fetcher, parse *parser.Parser, robot *robots.Robots, analyze *analyzer.Analyzer, embed *embedder.Embedder, db *store.Store) {
	log.Println("=== starting new crawl cycle ===")

	f := frontier.New()
	for _, seed := range seeds {
		f.Add(seed)
	}

	var pagesCount atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(f, fetch, parse, &pagesCount, &wg, robot, analyze, embed, db)
	}
	wg.Wait()

	log.Printf("=== crawl cycle finished, %d pages fetched ===\n", pagesCount.Load())
}

func worker(f *frontier.Frontier, fetch *fetcher.Fetcher, parse *parser.Parser, pagesCount *atomic.Int64, wg *sync.WaitGroup, robot *robots.Robots, analyze *analyzer.Analyzer, embed *embedder.Embedder, db *store.Store) {
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
			newText = ""
		}

		// shouldIndex tracks whether this page's content needs (re)embedding -
		// true if it's brand new, or if analyzer confirms a meaningful change.
		// False (default) means "unchanged - existing chunks are still valid,
		// don't waste Voyage API calls or create duplicate chunks."
		shouldIndex := false

		prev, found, err := db.GetLatestSnapshot(rawurl)
		if err != nil {
			log.Printf("history lookup error: %v", err)
		} else if !found {
			log.Printf("first time seeing %s - indexing, nothing to compare yet", rawurl)
			shouldIndex = true
		} else if newText != "" && prev.ExtractedText != "" {
			result, err := analyze.CompareSnapshots(prev.ExtractedText, newText)
			if err != nil {
				log.Printf("analyzer error for %s: %v", rawurl, err)
			} else if result.Changed {
				log.Printf(">>> CHANGE DETECTED on %s: %s", rawurl, result.Summary)
				shouldIndex = true
			}
		}

		if shouldIndex && newText != "" {
			indexPage(rawurl, newText, embed, db)
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
			compressedBody = nil
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

// indexPage chunks a page's text, embeds each chunk via Voyage, and
// saves the results - the "indexing" half of Phase 7's RAG pipeline.
func indexPage(rawurl string, text string, embed *embedder.Embedder, db *store.Store) {
	pieces := embedder.ChunkText(text, chunkSize, chunkOverlap)
	if len(pieces) == 0 {
		return
	}

	vectors, err := embed.GenerateEmbeddings(pieces, "document")
	if err != nil {
		log.Printf("embedding error for %s: %v", rawurl, err)
		return
	}

	now := time.Now()
	chunks := make([]store.Chunk, len(pieces))
	for i, piece := range pieces {
		chunks[i] = store.Chunk{
			URL:       rawurl,
			Text:      piece,
			Embedding: vectors[i],
			CreatedAt: now,
		}
	}

	db.SaveChunks(chunks)
	log.Printf("indexed %s (%d chunks)", rawurl, len(chunks))
}

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

func gzipDecompress(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	return io.ReadAll(gz)
}