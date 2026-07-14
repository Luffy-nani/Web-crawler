package parser

import (
	"bytes"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

type Parser struct{}

func New() *Parser {
	return &Parser{}
}

// ExtractLinks parses an HTML document and returns all absolute HTTP/HTTPS links.
func (p *Parser) ExtractLinks(baseURL string, body []byte) ([]string, error) {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	var links []string
	p.walk(root, base, &links)

	return links, nil
}

func (p *Parser) walk(node *html.Node, base *url.URL, links *[]string) {
	if node.Type == html.ElementNode && node.Data == "a" {

		for _, attr := range node.Attr {

			if attr.Key != "href" {
				continue
			}

			raw := strings.TrimSpace(attr.Val)

			if raw == "" || raw == "#" {
				continue
			}

			// Ignore fragments
			raw = strings.Split(raw, "#")[0]

			if raw == "" {
				continue
			}

			ref, err := url.Parse(raw)
			if err != nil {
				continue
			}

			absolute := base.ResolveReference(ref)

			// Only keep HTTP/HTTPS links
			if absolute.Scheme != "http" && absolute.Scheme != "https" {
				continue
			}

			*links = append(*links, absolute.String())
		}
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		p.walk(child, base, links)
	}
}