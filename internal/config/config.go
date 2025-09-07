package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	} `yaml:"server"`
	Database struct {
		Neo4j struct {
			URI      string `yaml:"uri"`
			Username string `yaml:"username"`
			Password string `yaml:"password"`
		} `yaml:"neo4j"`
		Postgres struct {
			DSN string `yaml:"dsn"`
		} `yaml:"postgres"`
		Redis struct {
			Addr     string `yaml:"addr"`
			Password string `yaml:"password"`
			DB       int    `yaml:"db"`
		} `yaml:"redis"`
	} `yaml:"database"`
	Embedding struct {
		Provider string `yaml:"provider"` // "local", "cloud"
		Model    string `yaml:"model"`
		Endpoint string `yaml:"endpoint"`
		APIKey   string `yaml:"api_key"`
	} `yaml:"embedding"`
	Analysis struct {
		MaxFileSize     int64    `yaml:"max_file_size"`
		IgnorePatterns  []string `yaml:"ignore_patterns"`
		IncludeTests    bool     `yaml:"include_tests"`
		AnalyzeComments bool     `yaml:"analyze_comments"`
	} `yaml:"analysis"`
}

func Load(configPath string) (*Config, error) {
	var config Config

	config.Server.Host = "0.0.0.0"
	config.Server.Port = 8000
	config.Database.Postgres.DSN = getEnv("DATABASE_URL", "postgres://postgres:password@localhost:5432/vectordb?sslmode=disable")
	config.Database.Neo4j.URI = getEnv("NEO4J_URI", "bolt://localhost:7687")
	config.Database.Neo4j.Username = getEnv("NEO4J_USERNAME", "neo4j")
	config.Database.Neo4j.Password = getEnv("NEO4J_PASSWORD", "password")
	config.Database.Redis.Addr = getEnv("REDIS_ADDR", "localhost:6379")
	config.Database.Redis.Password = getEnv("REDIS_PASSWORD", "")
	config.Database.Redis.DB = 0
	config.Embedding.Provider = getEnv("EMBEDDING_PROVIDER", "local")
	config.Embedding.Model = getEnv("EMBEDDING_MODEL", "embeddinggemma-300m")
	config.Embedding.Endpoint = getEnv("EMBEDDING_ENDPOINT", "http://localhost:8080")
	config.Embedding.APIKey = getEnv("EMBEDDING_API_KEY", "")
	config.Analysis.MaxFileSize = 1024 * 1024 // 1MB
	config.Analysis.IgnorePatterns = []string{"node_modules", ".git", "vendor", "build", "dist"}
	config.Analysis.IncludeTests = true
	config.Analysis.AnalyzeComments = true

	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}

		if err := yaml.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	return &config, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
