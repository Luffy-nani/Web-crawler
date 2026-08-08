package store

import (
	"database/sql"
	"encoding/json"
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
	Err           string
}

// Chunk is one embedded piece of a page's text, ready for similarity search.
type Chunk struct {
	URL       string
	Text      string
	Embedding []float32
	CreatedAt time.Time
}

type Store struct {
	db          *sql.DB
	write       chan *Record
	writeChunks chan []Chunk
	wg          sync.WaitGroup
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

	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS chunks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT NOT NULL,
    chunk_text TEXT NOT NULL,
    embedding TEXT NOT NULL,
    created_at DATETIME NOT NULL
)
`)
	if err != nil {
		db.Close()
		return nil, err
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_pages_url ON pages(url)`)
	if err != nil {
		db.Close()
		return nil, err
	}

	s := &Store{
		db:          db,
		write:       make(chan *Record, 100),
		writeChunks: make(chan []Chunk, 100),
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

// SaveChunks queues a batch of chunks (typically all chunks from one
// page) to be written by the same single writer goroutine as Save().
func (s *Store) SaveChunks(chunks []Chunk) {
	s.writeChunks <- chunks
}

// writeLoop is the ONLY goroutine that ever touches s.db. It listens on
// BOTH channels via select, so page saves and chunk saves never race
// against each other - there's still exactly one writer, just handling
// two kinds of incoming work instead of one.
//
// The loop ends when BOTH channels are closed and drained - that's what
// the two "open" booleans track. A channel receive's second return value
// (ok) is false once a channel is closed AND empty, same signal you've
// used before with Frontier.Next().
func (s *Store) writeLoop() {
	recordsOpen, chunksOpen := true, true

	for recordsOpen || chunksOpen {
		select {
		case r, ok := <-s.write:
			if !ok {
				recordsOpen = false
				s.write = nil // nil channel blocks forever in select - stops this case from firing again
				continue
			}
			s.writeRecord(r)

		case c, ok := <-s.writeChunks:
			if !ok {
				chunksOpen = false
				s.writeChunks = nil
				continue
			}
			s.writeChunkBatch(c)
		}
	}
}

func (s *Store) writeRecord(r *Record) {
	_, err := s.db.Exec(`
		INSERT INTO pages (url, status_code, content_type, body, extracted_text, fetched_at, error)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, r.URL, r.StatusCode, r.ContentType, r.Body, r.ExtractedText, r.FetchedAt, r.Err)
	if err != nil {
		log.Printf("failed to save record: %v", err)
	}
}

func (s *Store) writeChunkBatch(chunks []Chunk) {
	tx, err := s.db.Begin()
	if err != nil {
		log.Printf("failed to begin chunk transaction: %v", err)
		return
	}

	stmt, err := tx.Prepare(`
		INSERT INTO chunks (url, chunk_text, embedding, created_at)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		log.Printf("failed to prepare chunk insert: %v", err)
		tx.Rollback()
		return
	}
	defer stmt.Close()

	for _, c := range chunks {
		embeddingJSON, err := json.Marshal(c.Embedding)
		if err != nil {
			log.Printf("failed to marshal embedding: %v", err)
			continue
		}
		if _, err := stmt.Exec(c.URL, c.Text, string(embeddingJSON), c.CreatedAt); err != nil {
			log.Printf("failed to save chunk: %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("failed to commit chunk transaction: %v", err)
	}
}

// Close signals no more writes are coming on EITHER channel, and blocks
// until writeLoop has drained both and fully exited.
func (s *Store) Close() error {
	close(s.write)
	close(s.writeChunks)
	s.wg.Wait()
	return s.db.Close()
}

// GetLatestSnapshot returns the most recent successful record for a URL.
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

// GetAllChunks loads every stored chunk, embeddings decoded and ready
// for similarity comparison. For a portfolio-scale project this is fine
// to load entirely into memory - a real vector DB would be the upgrade
// path if this ever needed to scale past that.
func (s *Store) GetAllChunks() ([]Chunk, error) {
	rows, err := s.db.Query(`SELECT url, chunk_text, embedding, created_at FROM chunks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []Chunk
	for rows.Next() {
		var c Chunk
		var embeddingJSON string
		if err := rows.Scan(&c.URL, &c.Text, &embeddingJSON, &c.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(embeddingJSON), &c.Embedding); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}