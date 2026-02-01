package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

func makeVec(values map[int]float32) []float32 {
	vec := make([]float32, 1024)
	for idx, val := range values {
		vec[idx] = val
	}
	return vec
}

func insertChunk(t *testing.T, db *sql.DB, text, source, section, parent string, headerLevel int, validAt string, embedding []float32) int64 {
	t.Helper()

	serialized, err := sqlite_vec.SerializeFloat32(embedding)
	if err != nil {
		t.Fatalf("serialize embedding: %v", err)
	}

	ingestedAt := time.Now().UTC().Format(time.RFC3339)
	var parentValue interface{}
	if parent != "" {
		parentValue = parent
	}
	var validValue interface{}
	if validAt != "" {
		validValue = validAt
	}

	res, err := db.Exec(
		`INSERT INTO chunks (text, source_file, section_title, header_level, parent_title, section_sequence, chunk_sequence, chunk_total, valid_at, ingested_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		text,
		source,
		section,
		headerLevel,
		parentValue,
		1,
		1,
		1,
		validValue,
		ingestedAt,
	)
	if err != nil {
		t.Fatalf("insert chunk: %v", err)
	}

	chunkID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	if _, err := db.Exec("INSERT INTO vec_chunks (chunk_id, embedding) VALUES (?, ?)", chunkID, serialized); err != nil {
		t.Fatalf("insert vec chunk: %v", err)
	}

	return chunkID
}

func newOllamaServer(t *testing.T, embedVec []float32) *httptest.Server {
	t.Helper()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/embed":
			vec := make([]float64, len(embedVec))
			for i, v := range embedVec {
				vec[i] = float64(v)
			}
			resp := map[string]any{
				"embeddings": [][]float64{vec},
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Fatalf("encode embed response: %v", err)
			}
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})

	return httptest.NewServer(handler)
}

func TestSearch(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer db.Close()

	vec1 := makeVec(map[int]float32{0: 1})
	vec2 := makeVec(map[int]float32{0: 1, 1: 1})
	vec3 := makeVec(map[int]float32{1: 1})

	id1 := insertChunk(t, db, "alpha", "a.md", "First", "", 2, "", vec1)
	id2 := insertChunk(t, db, "bravo", "b.md", "Second", "", 2, "", vec2)
	id3 := insertChunk(t, db, "charlie", "c.md", "Third", "", 2, "", vec3)

	server := newOllamaServer(t, vec1)
	defer server.Close()

	client := NewOllamaClient(server.URL, "embed")
	results, err := Search(db, client, "query", 3, "")
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if results[0].ID != int(id1) || results[1].ID != int(id2) || results[2].ID != int(id3) {
		t.Fatalf("unexpected search order: %v, %v, %v", results[0].ID, results[1].ID, results[2].ID)
	}
}

func TestSearchAsOf(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer db.Close()

	vec := makeVec(map[int]float32{0: 1})
	insertChunk(t, db, "timeless", "a.md", "First", "", 2, "", vec)
	insertChunk(t, db, "past", "b.md", "Second", "", 2, "2024-01-01", vec)
	insertChunk(t, db, "future", "c.md", "Third", "", 2, "2025-01-01", vec)

	server := newOllamaServer(t, vec)
	defer server.Close()

	client := NewOllamaClient(server.URL, "embed")
	results, err := Search(db, client, "query", 5, "2024-06-01")
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].ValidAt != "" || results[1].ValidAt != "2024-01-01" {
		t.Fatalf("unexpected as-of order: %q, %q", results[0].ValidAt, results[1].ValidAt)
	}
}

func TestSearchChronologicalOrder(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer db.Close()

	closeVec := makeVec(map[int]float32{0: 1})
	farVec := makeVec(map[int]float32{1: 1})

	insertChunk(t, db, "later", "later.md", "Later", "", 2, "2025-01-01", closeVec)
	insertChunk(t, db, "earlier", "earlier.md", "Earlier", "", 2, "2024-01-01", farVec)

	server := newOllamaServer(t, closeVec)
	defer server.Close()

	client := NewOllamaClient(server.URL, "embed")
	results, err := Search(db, client, "query", 5, "")
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].ValidAt != "2024-01-01" || results[1].ValidAt != "2025-01-01" {
		t.Fatalf("unexpected chronological order: %q, %q", results[0].ValidAt, results[1].ValidAt)
	}
}
