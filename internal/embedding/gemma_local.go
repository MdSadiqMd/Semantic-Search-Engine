package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type GemmaLocalService struct {
	endpoint   string
	model      string
	client     *http.Client
	dimensions int
}

type LocalEmbeddingRequest struct {
	Text  string `json:"text"`
	Model string `json:"model"`
}

type LocalEmbeddingResponse struct {
	Embedding []float32 `json:"embedding"`
	Error     string    `json:"error,omitempty"`
}

func NewGemmaLocalService(endpoint, model string) (*GemmaLocalService, error) {
	if endpoint == "" {
		endpoint = "http://localhost:8080"
	}
	if model == "" {
		model = "embeddinggemma-300m"
	}

	return &GemmaLocalService{
		endpoint: endpoint,
		model:    model,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		dimensions: 768,
	}, nil
}

func (g *GemmaLocalService) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return nil, fmt.Errorf("text cannot be empty")
	}

	req := LocalEmbeddingRequest{
		Text:  text,
		Model: g.model,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", g.endpoint+"/v1/embeddings", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var embResp LocalEmbeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if embResp.Error != "" {
		return nil, fmt.Errorf("embedding service error: %s", embResp.Error)
	}

	if len(embResp.Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding returned")
	}

	return embResp.Embedding, nil
}

func (g *GemmaLocalService) GetDimensions() int {
	return g.dimensions
}

func (g *GemmaLocalService) IsAvailable(ctx context.Context) bool {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", g.endpoint+"/health", nil)
	if err != nil {
		return false
	}

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

func (g *GemmaLocalService) Close() error {
	return nil
}

func (g *GemmaLocalService) GenerateBatchEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))

	batchSize := 10
	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}

		for j := i; j < end; j++ {
			embedding, err := g.GenerateEmbedding(ctx, texts[j])
			if err != nil {
				return nil, fmt.Errorf("failed to generate embedding for text %d: %w", j, err)
			}
			embeddings[j] = embedding
		}

		time.Sleep(100 * time.Millisecond)
	}

	return embeddings, nil
}
