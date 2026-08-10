package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webcrawler/internal/store"
)

func TestOverviewHandler(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "crawler.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New() error = %v", err)
	}

	now := time.Now()
	s.Save(&store.Record{
		URL:        "https://example.com/a",
		StatusCode: 200,
		FetchedAt:  now,
		Err:        "",
	})
	s.Save(&store.Record{
		URL:        "https://example.com/b",
		StatusCode: 0,
		FetchedAt:  now.Add(-time.Minute),
		Err:        "timeout",
	})
	s.SaveChange("https://example.com/a", "pricing updated")

	if err := s.Close(); err != nil {
		t.Fatalf("store.Close() error = %v", err)
	}

	s, err = store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New() reopen error = %v", err)
	}
	defer s.Close()

	app, err := newDashboardApp(s, func(string) (searchResult, error) {
		return searchResult{}, nil
	})
	if err != nil {
		t.Fatalf("newDashboardApp() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	app.overviewHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "https://example.com/a") {
		t.Fatalf("response missing tracked URL: %s", body)
	}
	if !strings.Contains(body, "pricing updated") {
		t.Fatalf("response missing change summary: %s", body)
	}
	if !strings.Contains(body, "error: timeout") {
		t.Fatalf("response missing errored status text: %s", body)
	}
}

func TestSearchHandler(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "crawler.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New() error = %v", err)
	}
	defer s.Close()

	app, err := newDashboardApp(s, func(question string) (searchResult, error) {
		if question != "what changed?" {
			t.Fatalf("question = %q, want %q", question, "what changed?")
		}
		return searchResult{
			Answer:  "Pricing increased.",
			Sources: []string{"https://example.com/pricing"},
		}, nil
	})
	if err != nil {
		t.Fatalf("newDashboardApp() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/search?q=what+changed%3F", nil)
	rec := httptest.NewRecorder()
	app.searchHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Pricing increased.") {
		t.Fatalf("response missing answer: %s", body)
	}
	if !strings.Contains(body, "https://example.com/pricing") {
		t.Fatalf("response missing source URL: %s", body)
	}
}
