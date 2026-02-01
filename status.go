package main

import (
	"context"
	"database/sql"
)

type StatusInfo struct {
	OllamaHealthy    bool
	EmbedModel       string
	SqliteVecVersion string
	TotalChunks      int
	EarliestValidAt  string
	LatestValidAt    string
}

// Status gathers system status information.
// It never returns an error — it returns whatever it can gather.
// embedModel is passed separately since OllamaClient fields are unexported.
func Status(db *sql.DB, ollama *OllamaClient, embedModel string) StatusInfo {
	info := StatusInfo{
		EmbedModel: embedModel,
	}

	// Check Ollama health
	ctx := context.Background()
	info.OllamaHealthy = ollama.IsHealthy(ctx)

	// Get sqlite-vec version
	var vecVersion string
	err := db.QueryRow("SELECT vec_version()").Scan(&vecVersion)
	if err == nil {
		info.SqliteVecVersion = vecVersion
	}

	// Count total chunks
	var totalChunks int
	err = db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&totalChunks)
	if err == nil {
		info.TotalChunks = totalChunks
	}

	// Get earliest valid_at (ignoring NULLs)
	var earliestValidAt sql.NullString
	err = db.QueryRow("SELECT MIN(valid_at) FROM chunks WHERE valid_at IS NOT NULL").Scan(&earliestValidAt)
	if err == nil && earliestValidAt.Valid {
		info.EarliestValidAt = earliestValidAt.String
	}

	// Get latest valid_at (ignoring NULLs)
	var latestValidAt sql.NullString
	err = db.QueryRow("SELECT MAX(valid_at) FROM chunks WHERE valid_at IS NOT NULL").Scan(&latestValidAt)
	if err == nil && latestValidAt.Valid {
		info.LatestValidAt = latestValidAt.String
	}

	return info
}
