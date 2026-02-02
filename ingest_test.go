package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMarkdownH2Only(t *testing.T) {
	content := "## First\nAlpha content.\n\n## Second\nBeta content."
	sections := ParseMarkdown(content)

	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}
	if sections[0].Title != "First" || sections[0].HeaderLevel != 2 || sections[0].ParentTitle != "" {
		t.Fatalf("unexpected first section: %+v", sections[0])
	}
	if sections[0].Content != "Alpha content." {
		t.Fatalf("unexpected first content: %q", sections[0].Content)
	}
	if sections[1].Title != "Second" || sections[1].Content != "Beta content." {
		t.Fatalf("unexpected second section: %+v", sections[1])
	}
}

func TestParseMarkdownH3Preferred(t *testing.T) {
	content := strings.Join([]string{
		"## Architecture Decisions",
		"Context and constraints.",
		"",
		"### Database Selection",
		"We compared storage engines and chose the baseline.",
		"",
		"### API Design",
		"We defined request shapes and response contracts.",
		"",
		"## Implementation Notes",
		"This section has no ### children, so it's one chunk.",
	}, "\n")

	sections := ParseMarkdown(content)
	if len(sections) != 4 {
		t.Fatalf("expected 4 sections, got %d", len(sections))
	}

	if sections[0].Title != "Architecture Decisions" || sections[0].HeaderLevel != 2 || sections[0].Content != "Context and constraints." {
		t.Fatalf("unexpected preamble section: %+v", sections[0])
	}
	if sections[1].Title != "Database Selection" || sections[1].HeaderLevel != 3 || sections[1].ParentTitle != "Architecture Decisions" {
		t.Fatalf("unexpected first h3 section: %+v", sections[1])
	}
	if sections[2].Title != "API Design" || sections[2].HeaderLevel != 3 || sections[2].ParentTitle != "Architecture Decisions" {
		t.Fatalf("unexpected second h3 section: %+v", sections[2])
	}
	if sections[3].Title != "Implementation Notes" || sections[3].HeaderLevel != 2 {
		t.Fatalf("unexpected last h2 section: %+v", sections[3])
	}
}

func TestParseMarkdownPreamble(t *testing.T) {
	content := strings.Join([]string{
		"Preamble line one.",
		"Preamble line two.",
		"",
		"## Header",
		"Body.",
	}, "\n")

	sections := ParseMarkdown(content)
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}
	if sections[0].Title != "Preamble" || sections[0].HeaderLevel != 2 || sections[0].ParentTitle != "" {
		t.Fatalf("unexpected preamble section: %+v", sections[0])
	}
	if sections[1].Title != "Header" {
		t.Fatalf("unexpected header section: %+v", sections[1])
	}
}

func TestExtractDateFromHeader(t *testing.T) {
	tests := map[string]string{
		"## January 21, 2026":                             "2026-01-21",
		"## Summary — January 22, 2026 (Night Session)":   "2026-01-22",
		"## January 23, 2026 (Evening Session)":           "2026-01-23",
		"## Summary — January 24, 2026 (Morning Session)": "2026-01-24",
		"## Deployment Checklist (January 31, 2026)":      "2026-01-31",
		"## Database Selection":                           "",
		"## Summary":                                      "",
		"### Part 1: Authentication Flow":                 "",
	}

	for header, expected := range tests {
		if got := ExtractDateFromHeader(header); got != expected {
			t.Fatalf("expected %q for %q, got %q", expected, header, got)
		}
	}
}

func TestParseMarkdownWithDates(t *testing.T) {
	content := strings.Join([]string{
		"## January 21, 2026",
		"### Part 1: Authentication Flow",
		"One",
		"### Part 2: Caching Strategy",
		"Two",
		"## Summary",
		"Wrap",
		"## Deployment Checklist (January 31, 2026)",
		"Wish",
	}, "\n")

	sections := ParseMarkdown(content)
	if len(sections) != 4 {
		t.Fatalf("expected 4 sections, got %d", len(sections))
	}

	if sections[0].Title != "Part 1: Authentication Flow" || sections[0].ValidAt != "2026-01-21" {
		t.Fatalf("unexpected first section: %+v", sections[0])
	}
	if sections[1].Title != "Part 2: Caching Strategy" || sections[1].ValidAt != "2026-01-21" {
		t.Fatalf("unexpected second section: %+v", sections[1])
	}
	if sections[2].Title != "Summary" || sections[2].ValidAt != "" {
		t.Fatalf("unexpected summary section: %+v", sections[2])
	}
	if sections[3].Title != "Deployment Checklist (January 31, 2026)" || sections[3].ValidAt != "2026-01-31" {
		t.Fatalf("unexpected last section: %+v", sections[3])
	}
}

func TestChunkSection(t *testing.T) {
	section := Section{
		Title:       "Short",
		HeaderLevel: 2,
		Content:     "one two three four five",
		Sequence:    1,
	}
	chunks := ChunkSection(section, 600)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].ChunkSequence != 1 || chunks[0].ChunkTotal != 1 {
		t.Fatalf("unexpected chunk sequences: %+v", chunks[0])
	}
}

func TestChunkSectionOversized(t *testing.T) {
	paragraph := func(words int) string {
		parts := make([]string, words)
		for i := 0; i < words; i++ {
			parts[i] = "word"
		}
		return strings.Join(parts, " ")
	}

	content := strings.Join([]string{
		paragraph(300),
		paragraph(300),
		paragraph(300),
	}, "\n\n")

	section := Section{
		Title:       "Oversized",
		HeaderLevel: 2,
		Content:     content,
		Sequence:    2,
	}

	chunks := ChunkSection(section, 600)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].ChunkTotal != 2 || chunks[1].ChunkTotal != 2 {
		t.Fatalf("unexpected chunk totals: %+v %+v", chunks[0], chunks[1])
	}
}

func TestChunkSectionPreservesMetadata(t *testing.T) {
	content := strings.Join([]string{
		strings.Repeat("word ", 300),
		strings.Repeat("word ", 300),
		strings.Repeat("word ", 300),
	}, "\n\n")

	section := Section{
		Title:       "Parent",
		HeaderLevel: 3,
		ParentTitle: "Root",
		Content:     content,
		Sequence:    5,
	}

	chunks := ChunkSection(section, 600)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if chunk.SectionTitle != "Parent" || chunk.HeaderLevel != 3 || chunk.ParentTitle != "Root" || chunk.SectionSequence != 5 {
			t.Fatalf("metadata not preserved: %+v", chunk)
		}
	}
}

func TestIngestFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		embedding := make([]float64, EmbedDimension)
		embedding[0] = 0.42
		resp := embedResponse{Embeddings: [][]float64{embedding}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.md")
	content := strings.Join([]string{
		"## Architecture Decisions",
		"Context and constraints.",
		"",
		"### Database Selection",
		"We compared storage engines and chose the baseline.",
		"",
		"### API Design",
		"We defined request shapes and response contracts.",
		"",
		"## Implementation Notes",
		"This section has no ### children, so it's one chunk.",
	}, "\n")
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()

	client := NewOllamaClient(server.URL, "test-embed-model")
	result, err := IngestFile(db, client, filePath, "2024-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("IngestFile: %v", err)
	}
	if result.SectionsFound != 4 || result.ChunksCreated != 4 || result.SubChunksCreated != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	assertCount := func(query string, expected int) {
		t.Helper()
		var count int
		if err := db.QueryRow(query).Scan(&count); err != nil {
			t.Fatalf("query count: %v", err)
		}
		if count != expected {
			t.Fatalf("expected %d rows for %s, got %d", expected, query, count)
		}
	}

	assertCount("SELECT COUNT(*) FROM chunks", 4)
	assertCount("SELECT COUNT(*) FROM vec_chunks", 4)

	var storedSource string
	var storedValid sql.NullString
	var storedIngested string
	if err := db.QueryRow("SELECT source_file, valid_at, ingested_at FROM chunks LIMIT 1").Scan(&storedSource, &storedValid, &storedIngested); err != nil {
		t.Fatalf("query chunk row: %v", err)
	}
	if storedSource != filePath {
		t.Fatalf("expected source_file %q, got %q", filePath, storedSource)
	}
	if !storedValid.Valid || storedValid.String != "2024-01-01T00:00:00Z" {
		t.Fatalf("unexpected valid_at: %+v", storedValid)
	}
	if storedIngested == "" {
		t.Fatal("expected ingested_at to be set")
	}

}

func TestIngestFileSectionDates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		embedding := make([]float64, EmbedDimension)
		embedding[0] = 0.42
		resp := embedResponse{Embeddings: [][]float64{embedding}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.md")
	content := strings.Join([]string{
		"## January 21, 2026",
		"### Part 1: Authentication Flow",
		"The auth flow.",
		"### Part 2: Caching Strategy",
		"The cache details.",
		"## Summary",
		"Summary text.",
		"## January 30, 2026",
		"Standalone entry.",
	}, "\n")
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()

	client := NewOllamaClient(server.URL, "test-embed-model")
	result, err := IngestFile(db, client, filePath, "2024-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("IngestFile: %v", err)
	}
	if result.SectionsFound != 4 || result.ChunksCreated != 4 || result.SubChunksCreated != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	rows, err := db.Query("SELECT section_title, valid_at FROM chunks ORDER BY section_sequence, chunk_sequence")
	if err != nil {
		t.Fatalf("query chunks: %v", err)
	}
	defer rows.Close()

	type row struct {
		title   string
		validAt sql.NullString
	}
	results := []row{}
	for rows.Next() {
		var entry row
		if err := rows.Scan(&entry.title, &entry.validAt); err != nil {
			t.Fatalf("scan chunk: %v", err)
		}
		results = append(results, entry)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	expected := map[string]string{
		"Part 1: Authentication Flow": "2026-01-21",
		"Part 2: Caching Strategy":    "2026-01-21",
		"Summary":                     "2024-01-01T00:00:00Z",
		"January 30, 2026":            "2026-01-30",
	}

	if len(results) != len(expected) {
		t.Fatalf("expected %d chunks, got %d", len(expected), len(results))
	}

	for _, entry := range results {
		expectedValid, ok := expected[entry.title]
		if !ok {
			t.Fatalf("unexpected section_title %q", entry.title)
		}
		if !entry.validAt.Valid || entry.validAt.String != expectedValid {
			t.Fatalf("unexpected valid_at for %q: %+v", entry.title, entry.validAt)
		}
	}
}
