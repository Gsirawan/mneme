package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type OllamaClient struct {
	baseURL    string
	httpClient *http.Client
	embedModel string
}

func NewOllamaClient(baseURL, embedModel string) *OllamaClient {
	return &OllamaClient{
		baseURL:    baseURL,
		embedModel: embedModel,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// embedRequest is the request body for /api/embed
type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// embedResponse is the response from /api/embed
type embedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

// Embed calls Ollama /api/embed endpoint and returns a float32 vector
func (c *OllamaClient) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := embedRequest{
		Model: c.embedModel,
		Input: text,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("marshal embed request: %v", err)
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		log.Printf("create embed request: %v", err)
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("embed request failed: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("embed returned status %d: %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("embed returned status %d", resp.StatusCode)
	}

	var respData embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		log.Printf("decode embed response: %v", err)
		return nil, err
	}

	if len(respData.Embeddings) == 0 {
		log.Printf("embed response has no embeddings")
		return nil, fmt.Errorf("no embeddings in response")
	}

	// Convert first embedding from float64 to float32
	embedding := respData.Embeddings[0]
	result := make([]float32, len(embedding))
	for i, v := range embedding {
		result[i] = float32(v)
	}

	return result, nil
}

// generateRequest is the request body for /api/generate
type generateRequest struct {
	Model  string `json:"model"`
	System string `json:"system"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// generateResponse is the response from /api/generate
type generateResponse struct {
	Response string `json:"response"`
}

// GenerateAnswer calls Ollama /api/generate endpoint and returns response text
func (c *OllamaClient) GenerateAnswer(ctx context.Context, model, systemPrompt, userPrompt string) (string, error) {
	reqBody := generateRequest{
		Model:  model,
		System: systemPrompt,
		Prompt: userPrompt,
		Stream: false,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("marshal generate request: %v", err)
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		log.Printf("create generate request: %v", err)
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("generate request failed: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("generate returned status %d: %s", resp.StatusCode, string(body))
		return "", fmt.Errorf("generate returned status %d", resp.StatusCode)
	}

	var respData generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		log.Printf("decode generate response: %v", err)
		return "", err
	}

	return respData.Response, nil
}

// IsHealthy checks if Ollama is reachable by calling /api/tags
func (c *OllamaClient) IsHealthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/tags", nil)
	if err != nil {
		log.Printf("create health check request: %v", err)
		return false
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("health check request failed: %v", err)
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}
