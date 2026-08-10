package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"webcrawler/internal/analyzer"
	"webcrawler/internal/embedder"
	"webcrawler/internal/fetcher"
	"webcrawler/internal/frontier"
	"webcrawler/internal/metrics"
	"webcrawler/internal/parser"
	"webcrawler/internal/robots"
	"webcrawler/internal/store"
)

const maxPages = 50
const numWorkers = 5
const crawlInterval = 2 * time.Minute
const chunkSize = 200
const chunkOverlap = 40
const metricsAddr = ":2112" // Prometheus will scrape http://localhost:2112/metrics

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

	m, metricsHandler, err := metrics.New()
	if err != nil {
		log.Fatalf("failed to init metrics: %v", err)
	}

	// Serve /metrics in its own goroutine - Prometheus scrapes this over
	// HTTP on its own schedule, independent of the crawl loop.
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metricsHandler)
		log.Printf("metrics available at http://localhost%s/metrics", metricsAddr)
		if err := http.ListenAndServe(metricsAddr, mux); err != nil {
			log.Printf("metrics server error: %v", err)
		}
	}()

	seeds := []string{"https://example.com"}

	runCrawlCycle(seeds, fetch, parse, robot, analyze, embed, db, m)

	ticker := time.NewTicker(crawlInterval)
	defer ticker.Stop()

	for range ticker.C {
		runCrawlCycle(seeds, fetch, parse, robot, analyze, embed, db, m)
	}
}

func runCrawlCycle(seeds []string, fetch *fetcher.Fetcher, parse *parser.Parser, robot *robots.Robots, analyze *analyzer.Analyzer, embed *embedder.Embedder, db *store.Store, m *metrics.Metrics) {
	log.Println("=== starting new crawl cycle ===")

	f := frontier.New()
	for _, seed := range seeds {
		f.Add(seed)
	}

	m.SetFrontier(f) // queue-depth gauge now reads from THIS cycle's Frontier

	var pagesCount atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(f, fetch, parse, &pagesCount, &wg, robot, analyze, embed, db, m)
	}
	wg.Wait()

	log.Printf("=== crawl cycle finished, %d pages fetched ===\n", pagesCount.Load())
}

func worker(f *frontier.Frontier, fetch *fetcher.Fetcher, parse *parser.Parser, pagesCount *atomic.Int64, wg *sync.WaitGroup, robot *robots.Robots, analyze *analyzer.Analyzer, embed *embedder.Embedder, db *store.Store, m *metrics.Metrics) {
	defer wg.Done()
	ctx := context.Background() // no per-request context to thread through yet - fine for now

	for {
		rawurl, ok := f.Next()
		if !ok {
			return
		}

		fetchStart := time.Now()
		fresult, err := fetch.Fetch(rawurl)
		m.FetchDuration.Record(ctx, time.Since(fetchStart).Seconds())

		if err != nil {
			log.Printf("fetch error: %v", err)
			m.PagesFetched.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "error")))
			db.Save(&store.Record{
				URL:       rawurl,
				FetchedAt: time.Now(),
				Err:       err.Error(),
			})
			f.Done()
			continue
		}
		m.PagesFetched.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "success")))

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
				db.SaveChange(rawurl, result.Summary)
				shouldIndex = true
				m.ChangesDetected.Add(ctx, 1)
			}
		}

		if shouldIndex && newText != "" {
			indexPage(ctx, rawurl, newText, embed, db, m)
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

func indexPage(ctx context.Context, rawurl string, text string, embed *embedder.Embedder, db *store.Store, m *metrics.Metrics) {
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
	m.PagesIndexed.Add(ctx, 1)
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