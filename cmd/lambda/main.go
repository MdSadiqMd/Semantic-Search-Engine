package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/config"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/embedding"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/handlers"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/knowledgegraph"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/queue"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/search"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/storage"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	chiadapter "github.com/awslabs/aws-lambda-go-api-proxy/chi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"
)

var chiAdapter *chiadapter.ChiLambdaV2

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatal("Failed to initialize logger:", err)
	}
	defer logger.Sync()

	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		logger.Fatal("Failed to load config", zap.Error(err))
	}

	router, err := initializeRouter(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to initialize router", zap.Error(err))
	}

	chiAdapter = chiadapter.NewV2(router)
	lambda.Start(Handler)
}

func Handler(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	return chiAdapter.ProxyWithContextV2(ctx, req)
}

func initializeRouter(cfg *config.Config, logger *zap.Logger) (*chi.Mux, error) {
	postgresStorage, err := storage.NewPostgresStorage(cfg.Database.Postgres.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize postgres storage: %w", err)
	}

	neo4jStorage, err := storage.NewNeo4jStorage(
		cfg.Database.Neo4j.URI,
		cfg.Database.Neo4j.Username,
		cfg.Database.Neo4j.Password,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize neo4j storage: %w", err)
	}

	embeddingConfig := embedding.Config{
		Provider: cfg.Embedding.Provider,
		Model:    cfg.Embedding.Model,
		Endpoint: cfg.Embedding.Endpoint,
		APIKey:   cfg.Embedding.APIKey,
	}
	embeddingService, err := embedding.NewService(embeddingConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize embedding service: %w", err)
	}

	sqsQueue, err := queue.NewSQSQueue(os.Getenv("SQS_QUEUE_URL"), logger)
	if err != nil {
		logger.Fatal("SQS queue initialization failed", zap.Error(err))
	}

	dynamoPubSub, err := queue.NewDynamoPubSub(os.Getenv("CONNECTIONS_TABLE"), logger)
	if err != nil {
		logger.Fatal("DynamoDB initialization failed", zap.Error(err))
	}

	searchService := search.NewService(postgresStorage, embeddingService, logger)
	knowledgeGraphService := knowledgegraph.NewService(neo4jStorage, postgresStorage, logger)

	apiHandler := handlers.NewAPIHandler(
		postgresStorage,
		neo4jStorage,
		searchService,
		knowledgeGraphService,
		embeddingService,
		sqsQueue,
		dynamoPubSub,
		logger,
	)

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Route("/api", func(r chi.Router) {
		r.Get("/projects", apiHandler.ListProjects)
		r.Post("/projects", apiHandler.CreateProject)
		r.Get("/projects/{projectId}", apiHandler.GetProject)
		r.Put("/projects/{projectId}", apiHandler.UpdateProject)
		r.Delete("/projects/{projectId}", apiHandler.DeleteProject)

		r.Get("/projects/{projectId}/elements", apiHandler.ListCodeElements)
		r.Post("/projects/{projectId}/elements", apiHandler.CreateCodeElement)
		r.Get("/elements/{elementId}", apiHandler.GetCodeElement)
		r.Put("/elements/{elementId}", apiHandler.UpdateCodeElement)
		r.Delete("/elements/{elementId}", apiHandler.DeleteCodeElement)

		r.Post("/search", apiHandler.SearchSemantic)

		r.Get("/projects/{projectId}/graph", apiHandler.GetKnowledgeGraph)
		r.Get("/elements/{elementId}/connections", apiHandler.GetElementConnections)

		r.Post("/projects/{projectId}/analyze", apiHandler.StartAnalysis)
		r.Get("/jobs/{jobId}", apiHandler.GetAnalysisJob)
		r.Get("/projects/{projectId}/jobs", apiHandler.ListAnalysisJobs)

		r.Get("/projects/{projectId}/stats", apiHandler.GetProjectStats)

		r.Get("/health", apiHandler.HealthCheck)
	})

	return r, nil
}
