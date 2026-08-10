package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSaveChangeAndGetRecentChanges(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "crawler.db")

	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	s.SaveChange("https://example.com/a", "first change")
	time.Sleep(10 * time.Millisecond)
	s.SaveChange("https://example.com/a", "second change")

	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	s, err = New(dbPath)
	if err != nil {
		t.Fatalf("New() reopen error = %v", err)
	}
	defer s.Close()

	changes, err := s.GetRecentChanges(10)
	if err != nil {
		t.Fatalf("GetRecentChanges() error = %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("len(changes) = %d, want 2", len(changes))
	}
	if changes[0].Summary != "second change" {
		t.Fatalf("changes[0].Summary = %q, want %q", changes[0].Summary, "second change")
	}
	if changes[1].Summary != "first change" {
		t.Fatalf("changes[1].Summary = %q, want %q", changes[1].Summary, "first change")
	}

	limited, err := s.GetRecentChanges(1)
	if err != nil {
		t.Fatalf("GetRecentChanges(limit=1) error = %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("len(limited) = %d, want 1", len(limited))
	}
}

func TestGetTrackedPages(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "crawler.db")

	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	now := time.Now()
	s.Save(&Record{
		URL:        "https://example.com/a",
		StatusCode: 200,
		FetchedAt:  now.Add(-2 * time.Minute),
		Err:        "",
	})
	s.Save(&Record{
		URL:        "https://example.com/a",
		StatusCode: 500,
		FetchedAt:  now,
		Err:        "fetch failed",
	})
	s.Save(&Record{
		URL:        "https://example.com/b",
		StatusCode: 204,
		FetchedAt:  now.Add(-time.Minute),
		Err:        "",
	})

	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	s, err = New(dbPath)
	if err != nil {
		t.Fatalf("New() reopen error = %v", err)
	}
	defer s.Close()

	pages, err := s.GetTrackedPages()
	if err != nil {
		t.Fatalf("GetTrackedPages() error = %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("len(pages) = %d, want 2", len(pages))
	}
	if pages[0].URL != "https://example.com/a" {
		t.Fatalf("pages[0].URL = %q, want %q", pages[0].URL, "https://example.com/a")
	}
	if pages[0].StatusCode != 500 {
		t.Fatalf("pages[0].StatusCode = %d, want 500", pages[0].StatusCode)
	}
	if pages[0].Err != "fetch failed" {
		t.Fatalf("pages[0].Err = %q, want %q", pages[0].Err, "fetch failed")
	}
	if pages[1].URL != "https://example.com/b" {
		t.Fatalf("pages[1].URL = %q, want %q", pages[1].URL, "https://example.com/b")
	}
}
