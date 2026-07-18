package store

import (
	"database/sql"
	"log"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type Record struct {
	URL         string
	StatusCode  int
	Body        []byte
	ContentType string
	FetchedAt   time.Time
}

type Store struct {
	db    *sql.DB
	write chan *Record
	wg    sync.WaitGroup // lets Close() wait for writeLoop to fully drain
}

func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS pages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT NOT NULL,
    status_code INTEGER NOT NULL,
    content_type TEXT,
    body BLOB,
    fetched_at DATETIME NOT NULL
)
`)
	if err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{
		db:    db,
		write: make(chan *Record, 100),
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.writeLoop()
	}()
	return s, nil
}

func (s *Store) Save(r *Record) {
	s.write <- r
}

func (s *Store) writeLoop() {
	for r := range s.write {
		_, err := s.db.Exec(`
			INSERT INTO pages (url, status_code, content_type, body, fetched_at)
			VALUES (?, ?, ?, ?, ?)
		`, r.URL, r.StatusCode, r.ContentType, r.Body, r.FetchedAt)
		if err != nil {
			log.Printf("Failed to save record: %v", err)
		}
	}
}

func (s *Store) Close() error {
	close(s.write) // tells writeLoop's "for range" to stop once drained
	s.wg.Wait()     // BLOCKS here until writeLoop has actually finished
	return s.db.Close()
}