package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/config"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/embedding"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/handlers"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/knowledgegraph"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/queue"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/search"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.Load("config/config.yaml")
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

	searchService := search.NewService(postgresStorage, embeddingService, logger)
	knowledgeGraphService := knowledgegraph.NewService(neo4jStorage, postgresStorage, logger)

	apiHandler := handlers.NewAPIHandler(
		postgresStorage,
		neo4jStorage,
		searchService,
		knowledgeGraphService,
		embeddingService,
		redisQueue,
		redisPubSub,
		logger,
	)

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

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

	// Static file serving
	workDir, _ := os.Getwd()
	filesDir := http.Dir(workDir + "/dist")
	FileServer(r, "/", filesDir)

	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler: r,
	}

	wsHandler := handlers.NewWebSocketHandler(redisPubSub, logger)
	go wsHandler.Start()

	go func() {
		logger.Info("Starting API server",
			zap.String("host", cfg.Server.Host),
			zap.Int("port", cfg.Server.Port))

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Server failed to start", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server exited")
}

func FileServer(r chi.Router, path string, root http.FileSystem) {
	if path != "/" && path[len(path)-1] != '/' {
		r.Get(path, http.RedirectHandler(path+"/", http.StatusMovedPermanently).ServeHTTP)
		path += "/"
	}
	path += "*"

	r.Get(path, func(w http.ResponseWriter, r *http.Request) {
		rctx := chi.RouteContext(r.Context())
		pathPrefix := strings.TrimSuffix(rctx.RoutePattern(), "/*")
		fs := http.StripPrefix(pathPrefix, http.FileServer(root))
		fs.ServeHTTP(w, r)
	})
}
