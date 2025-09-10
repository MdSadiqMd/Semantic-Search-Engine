package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/ast"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/config"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/embedding"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/knowledgegraph"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/models"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/queue"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/storage"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatal("Failed to initialize logger:", err)
	}
	defer logger.Sync()

	postgresStorage, err := storage.NewPostgresStorage(cfg.Database.Postgres.DSN)
	if err != nil {
		logger.Fatal("Failed to initialize postgres storage", zap.Error(err))
	}
	defer postgresStorage.Close()

	neo4jStorage, err := storage.NewNeo4jStorage(
		cfg.Database.Neo4j.URI,
		cfg.Database.Neo4j.Username,
		cfg.Database.Neo4j.Password,
	)
	if err != nil {
		logger.Fatal("Failed to initialize neo4j storage", zap.Error(err))
	}
	defer neo4jStorage.Close()

	embeddingConfig := embedding.Config{
		Provider: cfg.Embedding.Provider,
		Model:    cfg.Embedding.Model,
		Endpoint: cfg.Embedding.Endpoint,
		APIKey:   cfg.Embedding.APIKey,
	}
	embeddingService, err := embedding.NewService(embeddingConfig)
	if err != nil {
		logger.Fatal("Failed to initialize embedding service", zap.Error(err))
	}
	defer embeddingService.Close()

	parserRegistry := ast.NewParserRegistry()
	knowledgeGraphService := knowledgegraph.NewService(neo4jStorage, postgresStorage, logger)

	redisQueue := queue.NewRedisQueue(
		cfg.Database.Redis.Addr,
		cfg.Database.Redis.Password,
		cfg.Database.Redis.DB,
		"analysis_jobs",
	)
	defer redisQueue.Close()

	redisPubSub := queue.NewRedisPubSub(
		cfg.Database.Redis.Addr,
		cfg.Database.Redis.Password,
		cfg.Database.Redis.DB,
	)
	defer redisPubSub.Close()

	worker := queue.NewWorker(redisQueue, logger)

	worker.RegisterHandler(queue.JobTypeAnalyzeProject, func(ctx context.Context, job *queue.Job) error {
		return handleAnalyzeProject(ctx, job, postgresStorage, neo4jStorage, parserRegistry, embeddingService, knowledgeGraphService, redisPubSub, logger)
	})

	worker.RegisterHandler(queue.JobTypeGenerateEmbedding, func(ctx context.Context, job *queue.Job) error {
		return handleGenerateEmbedding(ctx, job, postgresStorage, embeddingService, logger)
	})

	worker.RegisterHandler(queue.JobTypeExtractAST, func(ctx context.Context, job *queue.Job) error {
		return handleExtractAST(ctx, job, postgresStorage, parserRegistry, logger)
	})

	worker.RegisterHandler(queue.JobTypeBuildKnowledgeGraph, func(ctx context.Context, job *queue.Job) error {
		return handleBuildKnowledgeGraph(ctx, job, knowledgeGraphService, redisPubSub, logger)
	})

	go worker.Start()

	logger.Info("Worker started, waiting for jobs...")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down worker...")
	worker.Stop()
	logger.Info("Worker stopped")
}

func handleAnalyzeProject(
	ctx context.Context,
	job *queue.Job,
	postgresStorage *storage.PostgresStorage,
	neo4jStorage *storage.Neo4jStorage,
	parserRegistry *ast.ParserRegistry,
	embeddingService *embedding.Service,
	_ *knowledgegraph.Service,
	pubsub *queue.RedisPubSub,
	logger *zap.Logger,
) error {
	projectID, ok := job.Data["project_id"].(string)
	if !ok {
		return fmt.Errorf("invalid project_id in job data")
	}
	logger.Info("Starting project analysis", zap.String("project_id", projectID))

	project, err := postgresStorage.GetProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}

	elements, relationships, err := parserRegistry.ParseProject(ctx, project.Path, []string{
		"node_modules", ".git", "vendor", "build", "dist",
	})
	if err != nil {
		return fmt.Errorf("failed to parse project: %w", err)
	}

	logger.Info("Parsed project files",
		zap.String("project_id", projectID),
		zap.Int("elements", len(elements)),
		zap.Int("relationships", len(relationships)))

	for i, element := range elements {
		err := postgresStorage.CreateCodeElement(ctx, &element)
		if err != nil {
			logger.Error("Failed to store code element", zap.Error(err))
			continue
		}

		text := embedding.PrepareTextForEmbedding(map[string]interface{}{
			"name":        element.Name,
			"signature":   element.Signature,
			"doc_comment": element.DocComment,
			"code":        element.Code,
		})

		embeddingVec, err := embeddingService.GenerateEmbedding(ctx, text)
		if err != nil {
			logger.Error("Failed to generate embedding", zap.Error(err))
			continue
		}

		element.Embedding = embeddingVec
		_, err = postgresStorage.UpdateCodeElement(ctx, element.ID, map[string]interface{}{
			"embedding": embeddingVec,
		})
		if err != nil {
			logger.Error("Failed to update element with embedding", zap.Error(err))
		}

		err = neo4jStorage.CreateCodeElementNode(ctx, &element)
		if err != nil {
			logger.Error("Failed to create Neo4j node", zap.Error(err))
		}

		progress := (i + 1) * 100 / len(elements)
		pubsub.Publish(ctx, "analysis_updates", map[string]interface{}{
			"job_id":     job.ID,
			"project_id": projectID,
			"progress":   progress,
			"status":     "processing",
		})
	}

	for _, relationship := range relationships {
		err := postgresStorage.CreateRelationship(ctx, &relationship)
		if err != nil {
			logger.Error("Failed to store relationship", zap.Error(err))
			continue
		}

		err = neo4jStorage.CreateRelationshipEdge(ctx, &relationship)
		if err != nil {
			logger.Error("Failed to create Neo4j relationship", zap.Error(err))
		}
	}

	stats := calculateProjectStats(elements)
	_, err = postgresStorage.UpdateProject(ctx, projectID, map[string]interface{}{
		"statistics": stats,
	})
	if err != nil {
		logger.Error("Failed to update project stats", zap.Error(err))
	}

	pubsub.Publish(ctx, "analysis_updates", map[string]interface{}{
		"job_id":     job.ID,
		"project_id": projectID,
		"progress":   100,
		"status":     "completed",
	})

	logger.Info("Project analysis completed", zap.String("project_id", projectID))
	return nil
}

func handleGenerateEmbedding(
	ctx context.Context,
	job *queue.Job,
	postgresStorage *storage.PostgresStorage,
	embeddingService *embedding.Service,
	logger *zap.Logger,
) error {
	elementID, ok := job.Data["element_id"].(string)
	if !ok {
		return fmt.Errorf("invalid element_id in job data")
	}

	text, ok := job.Data["text"].(string)
	if !ok {
		return fmt.Errorf("invalid text in job data")
	}

	embeddingVec, err := embeddingService.GenerateEmbedding(ctx, text)
	if err != nil {
		return fmt.Errorf("failed to generate embedding: %w", err)
	}

	_, err = postgresStorage.UpdateCodeElement(ctx, elementID, map[string]interface{}{
		"embedding": embeddingVec,
	})
	if err != nil {
		return fmt.Errorf("failed to update element: %w", err)
	}

	logger.Info("Generated embedding for element", zap.String("element_id", elementID))
	return nil
}

func handleExtractAST(
	ctx context.Context,
	job *queue.Job,
	postgresStorage *storage.PostgresStorage,
	parserRegistry *ast.ParserRegistry,
	logger *zap.Logger,
) error {
	projectID, ok := job.Data["project_id"].(string)
	if !ok {
		return fmt.Errorf("invalid project_id in job data")
	}

	filePath, ok := job.Data["file_path"].(string)
	if !ok {
		return fmt.Errorf("invalid file_path in job data")
	}

	parser, err := parserRegistry.GetParser(filePath)
	if err != nil {
		return fmt.Errorf("no parser found for file: %w", err)
	}

	elements, relationships, err := parser.ParseFile(ctx, filePath)
	if err != nil {
		return fmt.Errorf("failed to parse file: %w", err)
	}

	for _, element := range elements {
		element.ProjectID = projectID
		err := postgresStorage.CreateCodeElement(ctx, &element)
		if err != nil {
			logger.Error("Failed to store code element", zap.Error(err))
		}
	}

	for _, relationship := range relationships {
		err := postgresStorage.CreateRelationship(ctx, &relationship)
		if err != nil {
			logger.Error("Failed to store relationship", zap.Error(err))
		}
	}

	logger.Info("Extracted AST from file",
		zap.String("file_path", filePath),
		zap.Int("elements", len(elements)),
		zap.Int("relationships", len(relationships)))

	return nil
}

func handleBuildKnowledgeGraph(
	ctx context.Context,
	job *queue.Job,
	knowledgeGraphService *knowledgegraph.Service,
	pubsub *queue.RedisPubSub,
	logger *zap.Logger,
) error {
	projectID, ok := job.Data["project_id"].(string)
	if !ok {
		return fmt.Errorf("invalid project_id in job data")
	}

	err := knowledgeGraphService.BuildGraph(ctx, projectID)
	if err != nil {
		return fmt.Errorf("failed to build knowledge graph: %w", err)
	}

	pubsub.Publish(ctx, "knowledge_graph_updates", map[string]interface{}{
		"project_id": projectID,
		"status":     "updated",
	})

	logger.Info("Built knowledge graph", zap.String("project_id", projectID))
	return nil
}

func calculateProjectStats(elements []models.CodeElement) map[string]interface{} {
	stats := map[string]interface{}{
		"total_elements": len(elements),
		"element_counts": make(map[string]int),
		"package_counts": make(map[string]int),
	}

	elementCounts := stats["element_counts"].(map[string]int)
	packageCounts := stats["package_counts"].(map[string]int)

	for _, element := range elements {
		elementCounts[string(element.Type)]++
		if element.Package != "" {
			packageCounts[element.Package]++
		}
	}

	return stats
}
