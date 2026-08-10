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
