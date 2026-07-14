package fetcher

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

type FetchResult struct {
	URL         string
	StatusCode  int
	Body        []byte
	ContentType string
}

type Fetcher struct {
	client *http.Client
}

func New() *Fetcher {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 10,
		},
	}

	return &Fetcher{
		client: client,
	}
}

func (f *Fetcher) Fetch(URL string) (*FetchResult, error) {
	req, err := http.NewRequest("GET", URL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "VenkatCrawler/1.0")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPError{URL: URL, StatusCode: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024)) // read only 5mb
	if err != nil {
		return nil, err
	}

	contentType := resp.Header.Get("Content-Type")

	return &FetchResult{
		URL:         URL,
		StatusCode:  resp.StatusCode,
		Body:        body,
		ContentType: contentType,
	}, nil
}

type HTTPError struct {
	URL        string
	StatusCode int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("fetch %s: unexpected status %d", e.URL, e.StatusCode)
}
