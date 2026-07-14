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
	resp, err := f.client.Get(URL)
	if err != nil {
		return &FetchResult{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
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