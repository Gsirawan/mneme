package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

var EmbedDimension = 1024

func init() {
	sqlite_vec.Auto()
}

func loadEmbedDimension() {
	if dim := os.Getenv("EMBED_DIM"); dim != "" {
		if d, err := strconv.Atoi(dim); err == nil && d > 0 {
			EmbedDimension = d
		}
	}
}

func buildSchema(dim int) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS chunks (
    id INTEGER PRIMARY KEY,
    text TEXT NOT NULL,
    source_file TEXT NOT NULL,
    section_title TEXT NOT NULL,
    header_level INTEGER NOT NULL DEFAULT 2,
    parent_title TEXT,
    section_sequence INTEGER,
    chunk_sequence INTEGER,
    chunk_total INTEGER,
    valid_at TEXT,
    ingested_at TEXT NOT NULL,
    UNIQUE(source_file, section_sequence, chunk_sequence)
);

CREATE VIRTUAL TABLE IF NOT EXISTS vec_chunks USING vec0(
    chunk_id INTEGER PRIMARY KEY,
    embedding float[%d] distance_metric=cosine
);

-- Phase 2: Messages table for raw conversation storage
CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    text TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_session_ts ON messages(session_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id);

-- Phase 2: Vector search on messages (search actual words, not compressed topics)
CREATE VIRTUAL TABLE IF NOT EXISTS vec_messages USING vec0(
    message_id TEXT PRIMARY KEY,
    embedding float[%d] distance_metric=cosine
);
`, dim, dim)
}

var fts5Available = false

// FTS5 schema - run separately because CREATE VIRTUAL TABLE IF NOT EXISTS
// doesn't work well with FTS5 in all SQLite versions
func ensureFTS5(db *sql.DB) error {
	// Check if FTS5 table already exists
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='messages_fts'`).Scan(&name)
	if err == nil {
		fts5Available = true
		return nil // already exists
	}

	// Try to create FTS5 table - may fail if FTS5 not compiled in
	_, err = db.Exec(`
		CREATE VIRTUAL TABLE messages_fts USING fts5(
			message_id UNINDEXED,
			role,
			text,
			content=messages,
			content_rowid=rowid
		)
	`)
	if err != nil {
		// FTS5 not available - that's okay, we'll use LIKE fallback
		log.Printf("FTS5 not available (optional): %v", err)
		return nil
	}

	fts5Available = true

	// Populate from existing messages
	_, _ = db.Exec(`
		INSERT INTO messages_fts(message_id, role, text)
		SELECT id, role, text FROM messages
	`)

	return nil
}

func ValidateEmbedDimension(ollama *OllamaClient) error {
	ctx := context.Background()
	embedding, err := ollama.Embed(ctx, "dimension check")
	if err != nil {
		return fmt.Errorf("embed test failed: %w", err)
	}
	if len(embedding) != EmbedDimension {
		return fmt.Errorf("embedding model produces %d dimensions, config expects %d — set EMBED_DIM=%d in .env", len(embedding), EmbedDimension, len(embedding))
	}
	return nil
}

func InitDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, err
	}

	if _, err := db.Exec(buildSchema(EmbedDimension)); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Set up FTS5
	if err := ensureFTS5(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

// ============ Message Functions ============

// insertMessages upserts messages and their embeddings
func insertMessages(db *sql.DB, ollama *OllamaClient, messages []textMessage) (int, error) {
	if len(messages) == 0 {
		return 0, nil
	}

	ctx := context.Background()
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	msgStmt, err := tx.Prepare(`INSERT OR IGNORE INTO messages (id, session_id, role, timestamp, text) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("prepare msg: %w", err)
	}
	defer msgStmt.Close()

	var ftsStmt *sql.Stmt
	if fts5Available {
		ftsStmt, err = tx.Prepare(`INSERT OR IGNORE INTO messages_fts (message_id, role, text) VALUES (?, ?, ?)`)
		if err != nil {
			// FTS5 might have become unavailable, continue without it
			ftsStmt = nil
		} else {
			defer ftsStmt.Close()
		}
	}

	inserted := 0
	var toEmbed []textMessage

	for _, m := range messages {
		if m.MessageID == "" {
			continue
		}
		res, err := msgStmt.Exec(m.MessageID, m.SessionID, m.Role, m.Timestamp.UnixMilli(), m.Text)
		if err != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
			toEmbed = append(toEmbed, m)
			// Also insert into FTS if available
			if ftsStmt != nil {
				_, _ = ftsStmt.Exec(m.MessageID, m.Role, m.Text)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	// Embed new messages (outside transaction for performance)
	for _, m := range toEmbed {
		if len(m.Text) < 10 {
			continue // skip very short messages
		}
		embedding, err := ollama.Embed(ctx, m.Text)
		if err != nil {
			continue
		}
		serialized, err := sqlite_vec.SerializeFloat32(embedding)
		if err != nil {
			continue
		}
		_, _ = db.Exec(`INSERT OR IGNORE INTO vec_messages (message_id, embedding) VALUES (?, ?)`, m.MessageID, serialized)
	}

	return inserted, nil
}

// contextMessage for returning message context
type contextMessage struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
	Timestamp int64  `json:"timestamp"`
	Text      string `json:"text"`
}

// getMessageContext returns messages around a given message ID within rangeMinutes
func getMessageContext(db *sql.DB, messageID string, rangeMinutes int) ([]contextMessage, error) {
	var sessionID string
	var ts int64
	err := db.QueryRow(`SELECT session_id, timestamp FROM messages WHERE id = ?`, messageID).Scan(&sessionID, &ts)
	if err != nil {
		return nil, fmt.Errorf("message not found: %s", messageID)
	}

	rows, err := db.Query(`
		SELECT id, session_id, role, timestamp, text FROM messages
		WHERE session_id = ? AND timestamp BETWEEN ? AND ?
		ORDER BY timestamp ASC`,
		sessionID,
		ts-int64(rangeMinutes)*60*1000,
		ts+int64(rangeMinutes)*60*1000,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []contextMessage
	for rows.Next() {
		var m contextMessage
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Timestamp, &m.Text); err != nil {
			continue
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// ============ Search Functions ============

// MessageSearchResult for returning message search results
type MessageSearchResult struct {
	MessageID string  `json:"message_id"`
	SessionID string  `json:"session_id"`
	Role      string  `json:"role"`
	Timestamp int64   `json:"timestamp"`
	Text      string  `json:"text"`
	Distance  float64 `json:"distance"`
}

// searchMessages performs semantic search on messages
func searchMessages(db *sql.DB, ollama *OllamaClient, query string, limit int) ([]MessageSearchResult, error) {
	ctx := context.Background()
	embedding, err := ollama.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	serialized, err := sqlite_vec.SerializeFloat32(embedding)
	if err != nil {
		return nil, fmt.Errorf("serialize: %w", err)
	}

	rows, err := db.Query(`
		SELECT vm.message_id, m.session_id, m.role, m.timestamp, m.text, vm.distance
		FROM vec_messages vm
		JOIN messages m ON m.id = vm.message_id
		WHERE vm.embedding MATCH ? AND k = ?
		ORDER BY vm.distance ASC`,
		serialized, limit)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	var results []MessageSearchResult
	for rows.Next() {
		var r MessageSearchResult
		if err := rows.Scan(&r.MessageID, &r.SessionID, &r.Role, &r.Timestamp, &r.Text, &r.Distance); err != nil {
			continue
		}
		results = append(results, r)
	}
	return results, nil
}

// searchMessagesFTS performs exact phrase search using FTS5 or LIKE fallback
func searchMessagesFTS(db *sql.DB, query string, limit int) ([]MessageSearchResult, error) {
	var rows *sql.Rows
	var err error

	if fts5Available {
		// Use FTS5 for fast exact phrase matching
		rows, err = db.Query(`
			SELECT f.message_id, m.session_id, m.role, m.timestamp, m.text
			FROM messages_fts f
			JOIN messages m ON m.id = f.message_id
			WHERE messages_fts MATCH ?
			LIMIT ?`,
			query, limit)
	} else {
		// Fallback to LIKE for exact substring matching
		rows, err = db.Query(`
			SELECT id, session_id, role, timestamp, text
			FROM messages
			WHERE text LIKE ?
			ORDER BY timestamp DESC
			LIMIT ?`,
			"%"+query+"%", limit)
	}

	if err != nil {
		return nil, fmt.Errorf("text search: %w", err)
	}
	defer rows.Close()

	var results []MessageSearchResult
	for rows.Next() {
		var r MessageSearchResult
		if err := rows.Scan(&r.MessageID, &r.SessionID, &r.Role, &r.Timestamp, &r.Text); err != nil {
			continue
		}
		r.Distance = 0 // exact match
		results = append(results, r)
	}
	return results, nil
}

// searchMessagesWithContext performs semantic search and returns context window
func searchMessagesWithContext(db *sql.DB, ollama *OllamaClient, query string, limit, contextMinutes int) ([][]contextMessage, error) {
	results, err := searchMessages(db, ollama, query, limit)
	if err != nil {
		return nil, err
	}

	var contexts [][]contextMessage
	seen := make(map[string]bool) // avoid duplicate context windows

	for _, r := range results {
		// Skip if we already have context from this session+time range
		key := fmt.Sprintf("%s:%d", r.SessionID, r.Timestamp/(60000*int64(contextMinutes)))
		if seen[key] {
			continue
		}
		seen[key] = true

		ctx, err := getMessageContext(db, r.MessageID, contextMinutes)
		if err != nil || len(ctx) == 0 {
			continue
		}
		contexts = append(contexts, ctx)
	}
	return contexts, nil
}

// ============ Utility Functions ============

// countMessages returns total message count
func countMessages(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&count)
	return count, err
}

// countEmbeddedMessages returns count of messages with embeddings
func countEmbeddedMessages(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM vec_messages`).Scan(&count)
	return count, err
}

// sessionMessages groups messages by session
type sessionMessages struct {
	sessionID string
	messages  []textMessage
}

// readAllSessions reads all messages grouped by session
func readAllSessions(db *sql.DB) ([]sessionMessages, error) {
	rows, err := db.Query(`SELECT id, session_id, role, timestamp, text FROM messages ORDER BY session_id, timestamp ASC`)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	sessMap := make(map[string][]textMessage)
	var order []string

	for rows.Next() {
		var cm contextMessage
		if err := rows.Scan(&cm.ID, &cm.SessionID, &cm.Role, &cm.Timestamp, &cm.Text); err != nil {
			continue
		}
		if _, seen := sessMap[cm.SessionID]; !seen {
			order = append(order, cm.SessionID)
		}
		sessMap[cm.SessionID] = append(sessMap[cm.SessionID], textMessage{
			Role:      cm.Role,
			Text:      cm.Text,
			Timestamp: time.UnixMilli(cm.Timestamp),
			IsUser:    cm.Role == "Ghaith" || cm.Role == "Max" || cm.Role == "user",
			MessageID: cm.ID,
			SessionID: cm.SessionID,
		})
	}

	sessions := make([]sessionMessages, 0, len(order))
	for _, sid := range order {
		sessions = append(sessions, sessionMessages{
			sessionID: sid,
			messages:  sessMap[sid],
		})
	}
	return sessions, nil
}
