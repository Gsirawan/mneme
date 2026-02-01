package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatus(t *testing.T) {
	// Create a mock Ollama server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"models":[]}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Create OllamaClient pointing to mock server
	ollama := NewOllamaClient(server.URL, "embed-model")

	// Initialize in-memory database
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	// Insert test chunks with various valid_at dates
	testChunks := []struct {
		text       string
		validAt    string
		ingestedAt string
	}{
		{text: "First entry", validAt: "2025-01-10", ingestedAt: "2025-01-31"},
		{text: "Second entry", validAt: "2025-01-20", ingestedAt: "2025-01-31"},
		{text: "Third entry", validAt: "2025-01-25", ingestedAt: "2025-01-31"},
		{text: "Timeless entry", validAt: "", ingestedAt: "2025-01-31"},
	}

	for _, chunk := range testChunks {
		var validAt interface{} = chunk.validAt
		if chunk.validAt == "" {
			validAt = nil
		}
		_, err := db.Exec(
			`INSERT INTO chunks (text, source_file, section_title, section_sequence, valid_at, ingested_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			chunk.text, "test.md", "Test", 1, validAt, chunk.ingestedAt,
		)
		if err != nil {
			t.Fatalf("Insert chunk failed: %v", err)
		}
	}

	// Call Status
	status := Status(db, ollama, "embed-model")

	// Verify all fields are populated
	if !status.OllamaHealthy {
		t.Errorf("Expected OllamaHealthy=true, got false")
	}

	if status.EmbedModel != "embed-model" {
		t.Errorf("Expected EmbedModel='embed-model', got %q", status.EmbedModel)
	}

	if status.SqliteVecVersion == "" {
		t.Errorf("Expected SqliteVecVersion to be populated, got empty string")
	}

	if status.TotalChunks != 4 {
		t.Errorf("Expected TotalChunks=4, got %d", status.TotalChunks)
	}

	if status.EarliestValidAt != "2025-01-10" {
		t.Errorf("Expected EarliestValidAt='2025-01-10', got %q", status.EarliestValidAt)
	}

	if status.LatestValidAt != "2025-01-25" {
		t.Errorf("Expected LatestValidAt='2025-01-25', got %q", status.LatestValidAt)
	}
}

func TestStatusEmptyDB(t *testing.T) {
	// Create a mock Ollama server that returns unhealthy
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// Create OllamaClient pointing to mock server
	ollama := NewOllamaClient(server.URL, "embed-model")

	// Initialize in-memory database
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	// Call Status on empty database
	status := Status(db, ollama, "embed-model")

	// Verify it handles empty database gracefully
	if status.OllamaHealthy {
		t.Errorf("Expected OllamaHealthy=false for unhealthy server, got true")
	}

	if status.EmbedModel != "embed-model" {
		t.Errorf("Expected EmbedModel='embed-model', got %q", status.EmbedModel)
	}

	if status.SqliteVecVersion == "" {
		t.Errorf("Expected SqliteVecVersion to be populated, got empty string")
	}

	if status.TotalChunks != 0 {
		t.Errorf("Expected TotalChunks=0 for empty DB, got %d", status.TotalChunks)
	}

	if status.EarliestValidAt != "" {
		t.Errorf("Expected EarliestValidAt='' for empty DB, got %q", status.EarliestValidAt)
	}

	if status.LatestValidAt != "" {
		t.Errorf("Expected LatestValidAt='' for empty DB, got %q", status.LatestValidAt)
	}
}
