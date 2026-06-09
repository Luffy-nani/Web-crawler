package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/html"
)

var baseHost = "books.toscrape.com"
var seedUrl = "https://toscrape.com"

var visited = make(map[string]bool)
var mu sync.Mutex
var TotalPages int64
var workers = 3
var maxDepth = 3

type crawlJob struct {
	URL   *url.URL
	Depth int
}

var wg sync.WaitGroup
var ch = make(chan crawlJob, 200)

func main() {
	parsedSeed, err := url.Parse(seedUrl)
	if err != nil {
		fmt.Println("Error parsing seed URL:", err)
		return
	}

	start := time.Now()

	wg.Add(1)
	ch <- crawlJob{URL: parsedSeed, Depth: maxDepth}

	for i := 0; i < workers; i++ {
		go func() {
			for job := range ch {
				crawl(job.URL, job.Depth)
				wg.Done()
			}
		}()
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	wg.Wait()
	fmt.Println("Total Pages crawled:", atomic.LoadInt64(&TotalPages))
	fmt.Println("Total Time taken:   ", time.Since(start))
}

func crawl(currentURL *url.URL, depth int) {
	if depth == 0 {
		return
	}

	urlStr := currentURL.String()
	fmt.Println("Crawling depth:", depth, "->", urlStr)

	resp, err := http.Get(urlStr)
	if err != nil || resp.StatusCode != http.StatusOK {
		return
	}
	defer resp.Body.Close()

	atomic.AddInt64(&TotalPages, 1)

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return
	}

	walk(doc, currentURL, depth)
}

func walk(node *html.Node, baseURL *url.URL, depth int) {
	if node.Type == html.ElementNode && node.Data == "a" {
		for _, attr := range node.Attr {
			if attr.Key == "href" {
				rawUrl := strings.Split(attr.Val, "#")[0]
				if rawUrl == "" {
					continue
				}
				refURL, err := url.Parse(rawUrl)
				if err != nil {
					continue
				}
				absoluteURL := baseURL.ResolveReference(refURL)
				if absoluteURL.Host != baseHost {
					continue
				}
				fullStr := absoluteURL.String()

				mu.Lock()
				if !visited[fullStr] {
					visited[fullStr] = true
					mu.Unlock()

					if depth > 1 {
						wg.Add(1)
						go func(u *url.URL) {
							ch <- crawlJob{URL: u, Depth: depth - 1}
						}(absoluteURL)
					}
				} else {
					mu.Unlock()
				}
			}
		}
	}

	for c := node.FirstChild; c != nil; c = c.NextSibling {
		walk(c, baseURL, depth)
	}
}
