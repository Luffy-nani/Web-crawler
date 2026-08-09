package embedder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

const voyageURL = "https://api.voyageai.com/v1/embeddings"
const model = "voyage-4-lite"
const maxRetries = 4

type Embedder struct {
	client *http.Client
	apiKey string

	// mu serializes ALL embedding requests through this Embedder, even
	// though many workers call it concurrently. Voyage's API is sensitive
	// to BURSTS of simultaneous requests, not just total volume - so
	// turning concurrent calls into a sequential queue avoids that,
	// even while staying well under the raw requests-per-minute limit.
	mu sync.Mutex
}

func New() (*Embedder, error) {
	key := os.Getenv("VOYAGE_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("VOYAGE_API_KEY environment variable not set")
	}
	return &Embedder{
		client: &http.Client{Timeout: 30 * time.Second},
		apiKey: key,
	}, nil
}

type voyageRequest struct {
	Input     []string `json:"input"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type"`
}

type voyageResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

// GenerateEmbeddings sends one or more texts to Voyage and returns one
// vector per input text, in the same order. inputType should be
// "document" when embedding content to store, or "query" when embedding
// a search question.
func (e *Embedder) GenerateEmbeddings(texts []string, inputType string) ([][]float32, error) {
	// Only one embedding request in flight at a time across ALL callers -
	// see the mu comment above for why.
	e.mu.Lock()
	defer e.mu.Unlock()

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, 8s - gives Voyage's rate
			// limiter time to reset instead of hammering it again immediately.
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			time.Sleep(backoff)
		}

		vectors, statusCode, err := e.doEmbed(texts, inputType)
		if err == nil {
			return vectors, nil
		}
		lastErr = err

		// Only retry on 429 (rate limited) - any other error (bad request,
		// auth failure, etc) retrying won't help, so fail immediately.
		if statusCode != http.StatusTooManyRequests {
			return nil, err
		}
	}

	return nil, fmt.Errorf("gave up after %d retries: %w", maxRetries, lastErr)
}

// doEmbed makes exactly one attempt at the API call. Returns the HTTP
// status code alongside the error so the caller can decide whether a
// retry makes sense.
func (e *Embedder) doEmbed(texts []string, inputType string) ([][]float32, int, error) {
	reqBody := voyageRequest{
		Input:     texts,
		Model:     model,
		InputType: inputType,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", voyageURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("calling voyage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("voyage returned status %d", resp.StatusCode)
	}

	var result voyageResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode voyage response: %w", err)
	}

	vectors := make([][]float32, len(result.Data))
	for _, d := range result.Data {
		vectors[d.Index] = d.Embedding
	}
	return vectors, resp.StatusCode, nil
}