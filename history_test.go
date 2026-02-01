package main

import (
	"database/sql"
	"testing"
)

func TestHistory(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	// Insert test chunks with different valid_at dates
	testChunks := []struct {
		text         string
		sourceFile   string
		sectionTitle string
		parentTitle  string
		sectionSeq   int
		validAt      string
		ingestedAt   string
	}{
		{
			text:         "This is about learning Go",
			sourceFile:   "notes.md",
			sectionTitle: "Programming",
			parentTitle:  "Skills",
			sectionSeq:   1,
			validAt:      "2025-01-15",
			ingestedAt:   "2025-01-31",
		},
		{
			text:         "Go is a great language",
			sourceFile:   "notes.md",
			sectionTitle: "Languages",
			parentTitle:  "Tech",
			sectionSeq:   2,
			validAt:      "2025-01-20",
			ingestedAt:   "2025-01-31",
		},
		{
			text:         "Advanced Go patterns",
			sourceFile:   "notes.md",
			sectionTitle: "Advanced",
			parentTitle:  "Tech",
			sectionSeq:   3,
			validAt:      "2025-01-25",
			ingestedAt:   "2025-01-31",
		},
	}

	for _, chunk := range testChunks {
		_, err := db.Exec(
			`INSERT INTO chunks (text, source_file, section_title, parent_title, section_sequence, valid_at, ingested_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			chunk.text, chunk.sourceFile, chunk.sectionTitle, chunk.parentTitle, chunk.sectionSeq, chunk.validAt, chunk.ingestedAt,
		)
		if err != nil {
			t.Fatalf("Insert chunk failed: %v", err)
		}
	}

	// Search for "Go" - should return all 3 chunks in chronological order
	results, err := History(db, "Go", 10)
	if err != nil {
		t.Fatalf("History failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	// Verify chronological order
	expectedDates := []string{"2025-01-15", "2025-01-20", "2025-01-25"}
	for i, expected := range expectedDates {
		if results[i].ValidAt != expected {
			t.Errorf("Result %d: expected ValidAt %q, got %q", i, expected, results[i].ValidAt)
		}
	}
}

func TestHistoryNullDates(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	// Insert chunks with mixed NULL and non-NULL valid_at
	testChunks := []struct {
		text       string
		validAt    sql.NullString
		sectionSeq int
	}{
		{text: "Dated memory", validAt: sql.NullString{String: "2025-01-20", Valid: true}, sectionSeq: 2},
		{text: "Timeless memory", validAt: sql.NullString{Valid: false}, sectionSeq: 1},
		{text: "Another memory", validAt: sql.NullString{String: "2025-01-10", Valid: true}, sectionSeq: 3},
	}

	for _, chunk := range testChunks {
		_, err := db.Exec(
			`INSERT INTO chunks (text, source_file, section_title, section_sequence, valid_at, ingested_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			chunk.text, "test.md", "Test", chunk.sectionSeq, chunk.validAt, "2025-01-31",
		)
		if err != nil {
			t.Fatalf("Insert chunk failed: %v", err)
		}
	}

	results, err := History(db, "memory", 10)
	if err != nil {
		t.Fatalf("History failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	// First result should be the timeless one (NULL valid_at)
	if results[0].ValidAt != "" {
		t.Errorf("First result should be timeless (empty ValidAt), got %q", results[0].ValidAt)
	}

	// Then chronological by valid_at
	if results[1].ValidAt != "2025-01-10" {
		t.Errorf("Second result should be 2025-01-10, got %q", results[1].ValidAt)
	}

	if results[2].ValidAt != "2025-01-20" {
		t.Errorf("Third result should be 2025-01-20, got %q", results[2].ValidAt)
	}
}

func TestHistoryCaseInsensitive(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	// Insert chunks with different cases
	testChunks := []string{
		"Learning Go programming",
		"go is lowercase",
		"GO is uppercase",
		"GoLang mixed case",
	}

	for i, text := range testChunks {
		_, err := db.Exec(
			`INSERT INTO chunks (text, source_file, section_title, section_sequence, valid_at, ingested_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			text, "test.md", "Test", i, nil, "2025-01-31",
		)
		if err != nil {
			t.Fatalf("Insert chunk failed: %v", err)
		}
	}

	// Search with different cases
	results, err := History(db, "go", 10)
	if err != nil {
		t.Fatalf("History failed: %v", err)
	}

	if len(results) != 4 {
		t.Errorf("Expected 4 results for case-insensitive search, got %d", len(results))
	}
}

func TestHistoryAliases(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	testChunks := []struct {
		text       string
		sectionSeq int
	}{
		{text: "Alice mentioned the project deadline", sectionSeq: 1},
		{text: "Bob went to the office today", sectionSeq: 2},
		{text: "Roberto works on the backend", sectionSeq: 3},
		{text: "Charlie handles the frontend", sectionSeq: 4},
	}

	for _, chunk := range testChunks {
		_, err := db.Exec(
			`INSERT INTO chunks (text, source_file, section_title, section_sequence, valid_at, ingested_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			chunk.text, "test.md", "Test", chunk.sectionSeq, nil, "2025-01-31",
		)
		if err != nil {
			t.Fatalf("Insert chunk failed: %v", err)
		}
	}

	entityAliases = map[string][]string{}
	t.Cleanup(func() {
		entityAliases = map[string][]string{}
	})
	t.Setenv("MNEME_ALIASES", "alice=alice,bob,roberto")
	loadAliasesFromEnv()

	// Searching "Alice" should find Alice, Bob, and Roberto chunks (all aliases)
	results, err := History(db, "Alice", 10)
	if err != nil {
		t.Fatalf("History failed: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("Expected 3 results for 'Alice' (with aliases), got %d", len(results))
	}

	// Searching "Charlie" should find only Charlie chunk (no alias)
	results, err = History(db, "Charlie", 10)
	if err != nil {
		t.Fatalf("History failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result for 'Charlie' (no alias), got %d", len(results))
	}
}

func TestHistoryLimit(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	// Insert 10 chunks
	for i := 0; i < 10; i++ {
		_, err := db.Exec(
			`INSERT INTO chunks (text, source_file, section_title, section_sequence, valid_at, ingested_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			"test entry", "test.md", "Test", i, nil, "2025-01-31",
		)
		if err != nil {
			t.Fatalf("Insert chunk failed: %v", err)
		}
	}

	// Test explicit limit
	results, err := History(db, "test", 5)
	if err != nil {
		t.Fatalf("History failed: %v", err)
	}

	if len(results) != 5 {
		t.Errorf("Expected 5 results with limit=5, got %d", len(results))
	}

	// Test default limit (20) when limit <= 0
	results, err = History(db, "test", 0)
	if err != nil {
		t.Fatalf("History failed: %v", err)
	}

	if len(results) != 10 {
		t.Errorf("Expected 10 results (all available) with limit=0, got %d", len(results))
	}

	// Test negative limit defaults to 20
	results, err = History(db, "test", -1)
	if err != nil {
		t.Fatalf("History failed: %v", err)
	}

	if len(results) != 10 {
		t.Errorf("Expected 10 results (all available) with limit=-1, got %d", len(results))
	}
}
