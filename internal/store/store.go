package store

import (
	"database/sql"
	"log"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type Record struct {
	URL           string
	StatusCode    int
	Body          []byte
	ExtractedText string
	ContentType   string
	FetchedAt     time.Time
	Err           string // empty string means the fetch succeeded
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
    extracted_text TEXT,
    fetched_at DATETIME NOT NULL,
    error TEXT
)
`)
	if err != nil {
		db.Close()
		return nil, err
	}
	// Speeds up GetLatestSnapshot's "most recent row for this url" lookup -
	// without this, every call is a full table scan.
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_pages_url ON pages(url)`)
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
			INSERT INTO pages (url, status_code, content_type, body, extracted_text, fetched_at, error)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, r.URL, r.StatusCode, r.ContentType, r.Body, r.ExtractedText, r.FetchedAt, r.Err)
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

// GetLatestSnapshot returns the most recent successful record for a URL,
// if one exists. found=false means this URL has never been crawled before
// (or every prior attempt failed) - the caller should treat that as
// "nothing to compare against yet," not as an error.
func (s *Store) GetLatestSnapshot(url string) (record *Record, found bool, err error) {
	row := s.db.QueryRow(`
		SELECT url, status_code, content_type, body, extracted_text, fetched_at, error
		FROM pages
		WHERE url = ? AND error = ''
		ORDER BY fetched_at DESC
		LIMIT 1
	`, url)

	var r Record
	scanErr := row.Scan(&r.URL, &r.StatusCode, &r.ContentType, &r.Body, &r.ExtractedText, &r.FetchedAt, &r.Err)
	if scanErr == sql.ErrNoRows {
		return nil, false, nil
	}
	if scanErr != nil {
		return nil, false, scanErr
	}
	return &r, true, nil
}