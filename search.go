package main

import (
	"context"
	"database/sql"
	"sort"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

type SearchResult struct {
	ID           int
	Text         string
	SourceFile   string
	SectionTitle string
	ParentTitle  string
	HeaderLevel  int
	ValidAt      string
	Distance     float64
}

func Search(db *sql.DB, ollama *OllamaClient, query string, limit int, asOf string) ([]SearchResult, error) {
	ctx := context.Background()
	embedding, err := ollama.Embed(ctx, query)
	if err != nil {
		return nil, err
	}

	serialized, err := sqlite_vec.SerializeFloat32(embedding)
	if err != nil {
		return nil, err
	}

	fetchLimit := limit
	if asOf != "" {
		fetchLimit = limit * 3
	}

	rows, err := db.Query(
		`SELECT v.chunk_id, v.distance, c.text, c.source_file, c.section_title, c.parent_title, c.header_level, c.valid_at
		 FROM vec_chunks v
		 JOIN chunks c ON c.id = v.chunk_id
		 WHERE v.embedding MATCH ? AND v.k = ?
		 ORDER BY v.distance
		 LIMIT ?`,
		serialized,
		fetchLimit,
		fetchLimit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []SearchResult{}
	for rows.Next() {
		var result SearchResult
		var parentTitle sql.NullString
		var validAt sql.NullString
		if err := rows.Scan(
			&result.ID,
			&result.Distance,
			&result.Text,
			&result.SourceFile,
			&result.SectionTitle,
			&parentTitle,
			&result.HeaderLevel,
			&validAt,
		); err != nil {
			return nil, err
		}
		if parentTitle.Valid {
			result.ParentTitle = parentTitle.String
		}
		if validAt.Valid {
			result.ValidAt = validAt.String
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if asOf != "" {
		filtered := make([]SearchResult, 0, len(results))
		for _, result := range results {
			if result.ValidAt == "" || result.ValidAt <= asOf {
				filtered = append(filtered, result)
			}
		}
		results = filtered
	}

	if len(results) > limit {
		results = results[:limit]
	}

	sort.SliceStable(results, func(i, j int) bool {
		left := results[i].ValidAt
		right := results[j].ValidAt
		if left == "" && right == "" {
			return false
		}
		if left == "" {
			return true
		}
		if right == "" {
			return false
		}
		return left < right
	})

	return results, nil
}
