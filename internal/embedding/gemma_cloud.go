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

type GemmaCloudService struct {
	apiKey     string
	model      string
	endpoint   string
	client     *http.Client
	dimensions int
}

type CloudEmbeddingRequest struct {
	Input string `json:"input"`
	Model string `json:"model"`
}

type CloudEmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

func NewGemmaCloudService(apiKey, model string) (*GemmaCloudService, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required for cloud service")
	}
	if model == "" {
		model = "embeddinggemma-300m"
	}

	return &GemmaCloudService{
		apiKey:   apiKey,
		model:    model,
		endpoint: "https://api.google.com/v1/embeddings", // TODO: unsure
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		dimensions: 768,
	}, nil
}

func (g *GemmaCloudService) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return nil, fmt.Errorf("text cannot be empty")
	}

	req := CloudEmbeddingRequest{
		Input: text,
		Model: g.model,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", g.endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+g.apiKey)

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var embResp CloudEmbeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if embResp.Error != nil {
		return nil, fmt.Errorf("API error: %s (%s)", embResp.Error.Message, embResp.Error.Code)
	}

	if len(embResp.Data) == 0 || len(embResp.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding returned")
	}

	return embResp.Data[0].Embedding, nil
}

func (g *GemmaCloudService) GetDimensions() int {
	return g.dimensions
}

func (g *GemmaCloudService) IsAvailable(ctx context.Context) bool {
	testText := "test"
	_, err := g.GenerateEmbedding(ctx, testText)
	return err == nil
}

func (g *GemmaCloudService) Close() error {
	return nil
}

func (g *GemmaCloudService) GenerateBatchEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))

	rateLimiter := time.NewTicker(100 * time.Millisecond)
	defer rateLimiter.Stop()

	for i, text := range texts {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-rateLimiter.C:
			embedding, err := g.GenerateEmbedding(ctx, text)
			if err != nil {
				return nil, fmt.Errorf("failed to generate embedding for text %d: %w", i, err)
			}
			embeddings[i] = embedding
		}
	}

	return embeddings, nil
}

func (g *GemmaCloudService) GenerateEmbeddingWithRetry(ctx context.Context, text string, maxRetries int) ([]float32, error) {
	var lastErr error

	for i := 0; i <= maxRetries; i++ {
		embedding, err := g.GenerateEmbedding(ctx, text)
		if err == nil {
			return embedding, nil
		}

		lastErr = err

		if i < maxRetries {
			waitTime := time.Duration(1<<uint(i)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(waitTime):
				continue
			}
		}
	}

	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}
