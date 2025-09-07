package embedding

import (
	"context"
	"fmt"
)

type EmbeddingService interface {
	GenerateEmbedding(ctx context.Context, text string) ([]float32, error)
	GetDimensions() int
	IsAvailable(ctx context.Context) bool
}

type Service struct {
	local  *GemmaLocalService
	cloud  *GemmaCloudService
	config Config
}

type Config struct {
	Provider string // "local" or "cloud"
	Model    string
	Endpoint string
	APIKey   string
}

func NewService(config Config) (*Service, error) {
	service := &Service{config: config}

	switch config.Provider {
	case "local":
		local, err := NewGemmaLocalService(config.Endpoint, config.Model)
		if err != nil {
			return nil, fmt.Errorf("failed to create local embedding service: %w", err)
		}
		service.local = local
	case "cloud":
		cloud, err := NewGemmaCloudService(config.APIKey, config.Model)
		if err != nil {
			return nil, fmt.Errorf("failed to create cloud embedding service: %w", err)
		}
		service.cloud = cloud
	default:
		return nil, fmt.Errorf("unsupported provider: %s", config.Provider)
	}

	return service, nil
}

func (s *Service) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	switch s.config.Provider {
	case "local":
		if s.local == nil {
			return nil, fmt.Errorf("local embedding service not initialized")
		}
		return s.local.GenerateEmbedding(ctx, text)
	case "cloud":
		if s.cloud == nil {
			return nil, fmt.Errorf("cloud embedding service not initialized")
		}
		return s.cloud.GenerateEmbedding(ctx, text)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", s.config.Provider)
	}
}

func (s *Service) GetDimensions() int {
	switch s.config.Provider {
	case "local":
		if s.local == nil {
			return 0
		}
		return s.local.GetDimensions()
	case "cloud":
		if s.cloud == nil {
			return 0
		}
		return s.cloud.GetDimensions()
	default:
		return 0
	}
}

func (s *Service) IsAvailable(ctx context.Context) bool {
	switch s.config.Provider {
	case "local":
		if s.local == nil {
			return false
		}
		return s.local.IsAvailable(ctx)
	case "cloud":
		if s.cloud == nil {
			return false
		}
		return s.cloud.IsAvailable(ctx)
	default:
		return false
	}
}

func (s *Service) Close() error {
	if s.local != nil {
		return s.local.Close()
	}
	if s.cloud != nil {
		return s.cloud.Close()
	}
	return nil
}

func PrepareTextForEmbedding(element map[string]interface{}) string {
	var text string

	if name, ok := element["name"].(string); ok {
		text += name + " "
	}

	if signature, ok := element["signature"].(string); ok {
		text += signature + " "
	}

	if docComment, ok := element["doc_comment"].(string); ok {
		text += docComment + " "
	}

	if code, ok := element["code"].(string); ok {
		if len(code) > 2000 {
			code = code[:2000] + "..."
		}
		text += code
	}

	return text
}
