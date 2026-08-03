package embedder

import (
	"bytes"
	"os"
	"net/http"
	"fmt"
	"encoding/json"
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

func
