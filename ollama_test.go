package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbed(t *testing.T) {
	// Mock Ollama /api/embed endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("expected path /api/embed, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Verify request body
		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "test-embed-model" {
			t.Errorf("expected model 'test-embed-model', got %s", req.Model)
		}
		if req.Input != "test text" {
			t.Errorf("expected input 'test text', got %s", req.Input)
		}

		// Send response
		resp := embedResponse{
			Embeddings: [][]float64{
				{0.1, 0.2, 0.3, 0.4},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-embed-model")
	embedding, err := client.Embed(context.Background(), "test text")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(embedding) != 4 {
		t.Errorf("expected embedding length 4, got %d", len(embedding))
	}
	if embedding[0] != 0.1 || embedding[1] != 0.2 || embedding[2] != 0.3 || embedding[3] != 0.4 {
		t.Errorf("expected [0.1, 0.2, 0.3, 0.4], got %v", embedding)
	}
}

func TestEmbedMultipleEmbeddings(t *testing.T) {
	// Test that we take the first embedding when multiple are returned
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := embedResponse{
			Embeddings: [][]float64{
				{0.1, 0.2},
				{0.3, 0.4}, // This should be ignored
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-embed-model")
	embedding, err := client.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if embedding[0] != 0.1 || embedding[1] != 0.2 {
		t.Errorf("expected first embedding [0.1, 0.2], got %v", embedding)
	}
}

func TestEmbedEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := embedResponse{
			Embeddings: [][]float64{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-embed-model")
	_, err := client.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for empty embeddings, got nil")
	}
}

func TestEmbedHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-embed-model")
	_, err := client.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

func TestGenerateAnswer(t *testing.T) {
	// Mock Ollama /api/generate endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("expected path /api/generate, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Verify request body
		var req generateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "test-query-model" {
			t.Errorf("expected model 'test-query-model', got %s", req.Model)
		}
		if req.System != "You are helpful" {
			t.Errorf("expected system 'You are helpful', got %s", req.System)
		}
		if req.Prompt != "What is 2+2?" {
			t.Errorf("expected prompt 'What is 2+2?', got %s", req.Prompt)
		}
		if req.Stream != false {
			t.Errorf("expected stream false, got %v", req.Stream)
		}

		// Send response
		resp := generateResponse{
			Response: "The answer is 4",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-embed-model")
	answer, err := client.GenerateAnswer(context.Background(), "test-query-model", "You are helpful", "What is 2+2?")
	if err != nil {
		t.Fatalf("GenerateAnswer failed: %v", err)
	}

	if answer != "The answer is 4" {
		t.Errorf("expected 'The answer is 4', got %s", answer)
	}
}

func TestGenerateAnswerHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-embed-model")
	_, err := client.GenerateAnswer(context.Background(), "test-query-model", "system", "prompt")
	if err == nil {
		t.Fatal("expected error for HTTP 400, got nil")
	}
}

func TestIsHealthy(t *testing.T) {
	// Mock Ollama /api/tags endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("expected path /api/tags, got %s", r.URL.Path)
		}
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-embed-model")
	healthy := client.IsHealthy(context.Background())
	if !healthy {
		t.Fatal("expected IsHealthy to return true")
	}
}

func TestIsHealthyFail(t *testing.T) {
	// Test with no server running (connection refused)
	client := NewOllamaClient("http://localhost:9999", "test-embed-model")
	healthy := client.IsHealthy(context.Background())
	if healthy {
		t.Fatal("expected IsHealthy to return false when server unreachable")
	}
}

func TestIsHealthyHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "test-embed-model")
	healthy := client.IsHealthy(context.Background())
	if healthy {
		t.Fatal("expected IsHealthy to return false for non-200 status")
	}
}

func TestNewOllamaClient(t *testing.T) {
	client := NewOllamaClient("http://localhost:11434", "embed-model")
	if client.baseURL != "http://localhost:11434" {
		t.Errorf("expected baseURL 'http://localhost:11434', got %s", client.baseURL)
	}
	if client.embedModel != "embed-model" {
		t.Errorf("expected embedModel 'embed-model', got %s", client.embedModel)
	}
	if client.httpClient == nil {
		t.Fatal("expected httpClient to be initialized")
	}
	if client.httpClient.Timeout.Seconds() != 120 {
		t.Errorf("expected timeout 120s, got %v", client.httpClient.Timeout)
	}
}
