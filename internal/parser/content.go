package parser

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
)

type candidate struct {
	node       *html.Node
	textLength int
	linkLength int
}

var blockTags = map[string]bool{
	"div":     true,
	"article": true,
	"section": true,
	"main":    true,
	"p":       true,
}

var skipTags = map[string]bool{
	"script":   true,
	"style":    true,
	"noscript": true,
}

func (p *Parser) ExtractText(body []byte) (string, error) {
	root, err := html.Parse(bytes.NewReader(body)) //html.Parse returns a pointer to the root node of the parsed HTML document
	if err != nil {
		return "", err
	}

	var candidates []candidate
	collectCandidates(root, &candidates)

	if len(candidates) == 0 {
		return "", nil
	}

	best := findBestCandidate(candidates)
	if best == nil {
		return "", nil
	}

	text := plainText(best)
	return strings.TrimSpace(text), nil
}

// Walks the DOM and records every possible content block.
func collectCandidates(node *html.Node, candidates *[]candidate) {
	if node.Type == html.ElementNode && blockTags[node.Data] {
		*candidates = append(*candidates, candidate{
			node:       node,
			textLength: totalTextLen(node),
			linkLength: totalLinkLen(node),
		})
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectCandidates(child, candidates)
	}
}

// Counts all visible text recursively.
func totalTextLen(node *html.Node) int {
	if node.Type == html.ElementNode && skipTags[node.Data] {
		return 0
	}

	if node.Type == html.TextNode {
		return len(strings.TrimSpace(node.Data))
	}

	sum := 0
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		sum += totalTextLen(child)
	}

	return sum
}

// Counts text that is inside <a> tags.
func totalLinkLen(node *html.Node) int {
	return linkText(node, false)
}

func linkText(node *html.Node, insideLink bool) int {
	if node.Type == html.ElementNode && skipTags[node.Data] {
		return 0
	}

	if node.Type == html.ElementNode && node.Data == "a" {
		insideLink = true
	}

	if node.Type == html.TextNode {
		if insideLink {
			return len(strings.TrimSpace(node.Data))
		}
		return 0
	}

	sum := 0
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		sum += linkText(child, insideLink)
	}

	return sum
}

// Converts a subtree into plain text.
func plainText(node *html.Node) string {
	if node.Type == html.ElementNode && skipTags[node.Data] {
		return ""
	}

	if node.Type == html.TextNode {
		return node.Data
	}

	var sb strings.Builder

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		sb.WriteString(plainText(child))
		sb.WriteString(" ")
	}

	return sb.String()
}

// Chooses the highest-scoring content block.
func findBestCandidate(candidates []candidate) *html.Node {
	var best *candidate
	bestScore := 0.0

	for i := range candidates {
		c := &candidates[i]

		if c.textLength == 0 {
			continue
		}

		score := float64(c.textLength) *
			(1 - float64(c.linkLength)/float64(c.textLength))

		if best == nil || score > bestScore {
			best = c
			bestScore = score
		}
	}

	if best == nil {
		return nil
	}

	return best.node
}