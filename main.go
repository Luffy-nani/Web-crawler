package main

import (
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/net/html"
)

var url = "https://books.toscrape.com"
var m = make(map[string]bool)
var N = 10

func main() {
	crawl(url, N)
}

func crawl(url string, count int) {
	if count == 0 {
		return
	}
	resp, err := http.Get(url)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	docs, err := html.Parse(resp.Body)
	if err != nil {
		panic(err)
	}
	m[url] = true
	walk(docs, count)
}

func walk(node *html.Node, count int) {
	if node.Type == html.ElementNode && node.Data == "a" {
		for _, attr := range node.Attr {
			if attr.Key == "href" {
				baseUrl := attr.Val
				if strings.Contains(baseUrl, "..") {
					continue
				}
				if strings.HasPrefix(baseUrl, "https://") {
					if m[baseUrl] != true {
						fmt.Println("url: ", baseUrl)
						m[baseUrl] = true
						crawl(baseUrl, count-1)
					}
				} else if strings.HasPrefix(baseUrl, "/") {
					baseUrl = url + baseUrl
					if m[baseUrl] != true {
						fmt.Println("url: ", baseUrl)
						m[baseUrl] = true
						crawl(baseUrl, count-1)
					}
				} else {
					baseUrl = url + "/" + baseUrl
					if m[baseUrl] != true {
						fmt.Println("url: ", baseUrl)
						m[baseUrl] = true
						crawl(baseUrl, count-1)
					}
				}
			}

		}
	}

	for c := node.FirstChild; c != nil; c = c.NextSibling {
		walk(c, count)
	}
}
