package analyzer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const ollamaURL = "http://localhost:11434/api/generate"
const model = "llama3.2"

// ChangeResult is what YOU actually care about - parsed out of Ollama's
// response string.
type ChangeResult struct {
	Changed bool   `json:"changed"`
	Summary string `json:"summary"`
}

type Analyzer struct {
	client *http.Client
}

func New() *Analyzer {
	return &Analyzer{
		// Local LLM generation can be slow, especially without a GPU -
		// much longer timeout than the web Fetcher's 10s.
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// ollamaRequest is the body WE send.
type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
	Format string `json:"format"`
}

// ollamaEnvelope is Ollama's OUTER response wrapper. Response is a string
// containing more JSON text - a second layer that needs its own decode.
type ollamaEnvelope struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// CompareSnapshots asks the local LLM whether oldText and newText differ
// in a meaningful way, and returns a structured verdict.
func (a *Analyzer) CompareSnapshots(oldText, newText string) (*ChangeResult, error) {
	reqBody := ollamaRequest{
		Model:  model,
		Prompt: buildPrompt(oldText, newText),
		Stream: false,
		Format: "json",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := a.client.Post(ollamaURL, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("calling ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	// Layer 1: decode the outer envelope.
	var envelope ollamaEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode ollama envelope: %w", err)
	}

	// Layer 2: the envelope's Response field is ITSELF a JSON string -
	// decode that separately into what we actually want.
	var result ChangeResult
	if err := json.Unmarshal([]byte(envelope.Response), &result); err != nil {
		return nil, fmt.Errorf("decode inner json (raw: %s): %w", envelope.Response, err)
	}

	return &result, nil
}

func buildPrompt(oldText, newText string) string {
	return fmt.Sprintf(`You are comparing two versions of a webpage's content to detect meaningful changes.

Ignore trivial differences like whitespace, ad text, view counters, or timestamps.
Focus on substantive changes: pricing, features, policies, announcements, content additions or removals.

OLD VERSION:
%s

NEW VERSION:
%s

Respond with ONLY a JSON object in this exact shape, no other text:
{"changed": true or false, "summary": "one sentence describing what changed, or empty string if nothing changed"}`, oldText, newText)
}