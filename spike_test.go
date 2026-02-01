package main

import (
	"database/sql"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

func TestSQLiteVecSpike(t *testing.T) {
	sqlite_vec.Auto()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	var version string
	if err := db.QueryRow("SELECT vec_version()").Scan(&version); err != nil {
		t.Fatalf("vec_version(): %v", err)
	}
	if version == "" {
		t.Fatal("vec_version() returned empty string")
	}

	_, err = db.Exec("CREATE VIRTUAL TABLE test_vec USING vec0(embedding float[4] distance_metric=cosine)")
	if err != nil {
		t.Fatalf("create vec table: %v", err)
	}

	data := [][]float32{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
		{0, 0, 1, 0},
	}
	for i, vec := range data {
		serialized, err := sqlite_vec.SerializeFloat32(vec)
		if err != nil {
			t.Fatalf("serialize vec %d: %v", i+1, err)
		}
		_, err = db.Exec(
			"INSERT INTO test_vec(rowid, embedding) VALUES (?, ?)",
			i+1,
			serialized,
		)
		if err != nil {
			t.Fatalf("insert vec %d: %v", i+1, err)
		}
	}

	query := []float32{0.9, 0.1, 0, 0}
	queryBlob, err := sqlite_vec.SerializeFloat32(query)
	if err != nil {
		t.Fatalf("serialize query: %v", err)
	}
	var rowid int
	var distance float64
	err = db.QueryRow(
		"SELECT rowid, distance FROM test_vec WHERE embedding MATCH ? ORDER BY distance LIMIT 1",
		queryBlob,
	).Scan(&rowid, &distance)
	if err != nil {
		t.Fatalf("query nearest: %v", err)
	}
	if rowid != 1 {
		t.Fatalf("expected nearest rowid 1, got %d (distance=%f)", rowid, distance)
	}

	initDB, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	_ = initDB.Close()
}
