package main

import (
	"context"
	"database/sql"
	"fmt"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

const EmbedDimension = 1024

const schema = `CREATE TABLE IF NOT EXISTS chunks (
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
    embedding float[1024] distance_metric=cosine
);
`

func init() {
	sqlite_vec.Auto()
}

func ValidateEmbedDimension(ollama *OllamaClient) error {
	ctx := context.Background()
	embedding, err := ollama.Embed(ctx, "dimension check")
	if err != nil {
		return fmt.Errorf("embed test failed: %w", err)
	}
	if len(embedding) != EmbedDimension {
		return fmt.Errorf("embedding model produces %d dimensions, schema expects %d — change EMBED_MODEL or recreate database", len(embedding), EmbedDimension)
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

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}
