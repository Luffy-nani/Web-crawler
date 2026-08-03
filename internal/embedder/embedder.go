package embedder

import (
	"bytes"
	"os"
	"net/http"
	"fmt"
	"encoding/json"
	"time"
)

const voyageURL = "https://api.voyageai.com/v1/embeddings"
const model = "voyage-4-lite"

type Embedder struct {
	client *http.Client
	apiKey string
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
	InputType string   `json:"input_type"` // "document" or "query"
}

type voyageResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (e *Embedder) GenerateEmbeddings(texts []string, inputType string) ([][]float32, error)  {
	reqBody := voyageRequest{
		Input:     texts,
		Model:     model,
		InputType: inputType,
	}

	bodyBytes, err := json.Marshal(reqBody)  // to convert the request struct to JSON
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", voyageURL, bytes.NewReader(bodyBytes)) //bytes.NewReader(bodyBytes) creates a new reader from the byte slice, which is used as the request body
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling voyage: %w", err)
	}

	defer resp.Body.Close() // this is for ensuring that the response body is closed after we're done with it, to free up resources
		if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("voyage returned status %d", resp.StatusCode)
	}

	var result voyageResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode voyage response: %w", err)
	}

	vectors := make([][]float32, len(result.Data))
	for _, d := range result.Data {
		vectors[d.Index] = d.Embedding // use Index, not slice position - response order isn't guaranteed to match input order
	}
	return vectors, nil

}
