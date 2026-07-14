package fetcher

import (
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
		return &FetchResult{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024)) // read only 5mb
	if err != nil {
		return &FetchResult{}, err
	}

	contentType := resp.Header.Get("Content-Type")

	return &FetchResult{
		URL:         URL,
		StatusCode:  resp.StatusCode,
		Body:        body,
		ContentType: contentType,
	}, nil
}
