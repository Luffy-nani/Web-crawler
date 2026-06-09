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
var seedUrl = "https://toscrape.com" // Sandbox start page

var m = make(map[string]bool)
var N = 1
var TotalPages int64

var mu sync.RWMutex
var wg sync.WaitGroup

func main() {
	start := time.Now()

	parsedSeed, err := url.Parse(seedUrl)
	if err != nil {
		fmt.Println("Error parsing seed URL:", err)
		return
	}

	wg.Add(1)
	go crawl(parsedSeed, N)
	wg.Wait()

	timeTaken := time.Since(start)
	fmt.Println("\n====================================")
	fmt.Println("Total Pages crawled: ", atomic.LoadInt64(&TotalPages))
	fmt.Println("Total Time taken:     ", timeTaken)
}

func crawl(currentURL *url.URL, count int) {
	defer wg.Done()
	if count == 0 {
		return
	}

	urlStr := currentURL.String()
	fmt.Println("Crawling depth:", count, "->", urlStr)

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return // If network fails, url isn't permanently locked out
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	atomic.AddInt64(&TotalPages, 1)

	docs, err := html.Parse(resp.Body)
	if err != nil {
		return
	}

	// Walk through found items using the page's actual URL as the parsing base context
	walk(docs, currentURL, count)
}

func walk(node *html.Node, baseURL *url.URL, count int) {
	if node.Type == html.ElementNode && node.Data == "a" {
		for _, attr := range node.Attr {
			if attr.Key == "href" {
				// Clean fragment identifiers (e.g., #reviews) so you don't parse duplicates
				rawUrl := strings.Split(attr.Val, "#")[0]
				if rawUrl == "" {
					continue
				}

				refURL, err := url.Parse(rawUrl)
				if err != nil {
					continue
				}

				// MAGIC STEP: Converts absolute, root-relative, and directory ../ paths seamlessly
				absoluteURL := baseURL.ResolveReference(refURL)

				// Ensure we stay within the sandbox host domain boundaries
				if absoluteURL.Host != baseHost {
					continue
				}

				fullUrlStr := absoluteURL.String()

				// Synchronize checking and marking in one map transaction block
				mu.Lock()
				if m[fullUrlStr] {
					mu.Unlock()
					continue
				}
				m[fullUrlStr] = true
				mu.Unlock()

				wg.Add(1)
				go crawl(absoluteURL, count-1)
			}
		}
	}

	for c := node.FirstChild; c != nil; c = c.NextSibling {
		walk(c, baseURL, count)
	}
}
