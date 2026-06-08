package main

import (
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/net/html"
)

var url = "https://books.toscrape.com"
var m = make(map[string]bool)

func main() {
	crawl(url)
}

func crawl(url string) {
	resp, err := http.Get(url)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	docs, err := html.Parse(resp.Body)
	if err != nil {
		panic(err)
	}
	m[url]=true
	walk(docs)
}

func walk(node *html.Node) {
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
						crawl(baseUrl)
					}
				} else if strings.HasPrefix(baseUrl, "/") {
					baseUrl = url + baseUrl
					if m[baseUrl] != true {
						fmt.Println("url: ", baseUrl)
						m[baseUrl] = true
						crawl(baseUrl)
					}
				} else {
					baseUrl = url + "/" + baseUrl
					if m[baseUrl] != true {
						fmt.Println("url: ", baseUrl)
						m[baseUrl] = true
						crawl(baseUrl)
					}
				}
			}
		}
	}

	for c := node.FirstChild; c != nil; c = c.NextSibling {
		walk(c)
	}
}
