package main

import (
	"database/sql"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

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

func InitDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, err
	}

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}
