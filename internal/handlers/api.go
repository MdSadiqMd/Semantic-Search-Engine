package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/embedding"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/knowledgegraph"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/models"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/queue"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/search"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/storage"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/types"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"go.uber.org/zap"
)

type APIHandler struct {
	postgresStorage       *storage.PostgresStorage
	neo4jStorage          *storage.Neo4jStorage
	searchService         *search.Service
	knowledgeGraphService *knowledgegraph.Service
	embeddingService      *embedding.Service
	queue                 types.Queue
	pubsub                types.PubSub
	logger                *zap.Logger
}

func NewAPIHandler(
	postgresStorage *storage.PostgresStorage,
	neo4jStorage *storage.Neo4jStorage,
	searchService *search.Service,
	knowledgeGraphService *knowledgegraph.Service,
	embeddingService *embedding.Service,
	queue types.Queue,
	pubsub types.PubSub,
	logger *zap.Logger,
) *APIHandler {
	return &APIHandler{
		postgresStorage:       postgresStorage,
		neo4jStorage:          neo4jStorage,
		searchService:         searchService,
		knowledgeGraphService: knowledgeGraphService,
		embeddingService:      embeddingService,
		queue:                 queue,
		pubsub:                pubsub,
		logger:                logger,
	}
}

func (h *APIHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.postgresStorage.ListProjects(r.Context())
	if err != nil {
		h.logger.Error("Failed to list projects", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to list projects"})
		return
	}

	render.JSON(w, r, projects)
}

func (h *APIHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string              `json:"name"`
		Path       string              `json:"path"`
		Language   string              `json:"language"`
		Statistics models.ProjectStats `json:"statistics"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Invalid request body"})
		return
	}

	if req.Name == "" || req.Language == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Name and language are required"})
		return
	}

	project := &models.Project{
		Name:       req.Name,
		Path:       req.Path,
		Language:   req.Language,
		Statistics: req.Statistics,
	}

	err := h.postgresStorage.CreateProject(r.Context(), project)
	if err != nil {
		h.logger.Error("Failed to create project", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to create project"})
		return
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, project)
}

func (h *APIHandler) GetProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Project ID is required"})
		return
	}

	project, err := h.postgresStorage.GetProject(r.Context(), projectID)
	if err != nil {
		h.logger.Error("Failed to get project", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to get project"})
		return
	}

	if project == nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": "Project not found"})
		return
	}

	render.JSON(w, r, project)
}

func (h *APIHandler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Project ID is required"})
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Invalid request body"})
		return
	}

	project, err := h.postgresStorage.UpdateProject(r.Context(), projectID, updates)
	if err != nil {
		h.logger.Error("Failed to update project", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to update project"})
		return
	}

	render.JSON(w, r, project)
}

func (h *APIHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Project ID is required"})
		return
	}

	err := h.postgresStorage.DeleteProject(r.Context(), projectID)
	if err != nil {
		h.logger.Error("Failed to delete project", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to delete project"})
		return
	}

	render.Status(r, http.StatusNoContent)
	render.JSON(w, r, map[string]string{"message": "Project deleted successfully"})
}

func (h *APIHandler) ListCodeElements(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Project ID is required"})
		return
	}

	filters := map[string]interface{}{
		"project_id": projectID,
	}

	if elementType := r.URL.Query().Get("type"); elementType != "" {
		filters["type"] = elementType
	}
	if pkg := r.URL.Query().Get("package"); pkg != "" {
		filters["package"] = pkg
	}

	elements, err := h.postgresStorage.GetCodeElements(r.Context(), filters)
	if err != nil {
		h.logger.Error("Failed to list code elements", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to list code elements"})
		return
	}

	render.JSON(w, r, elements)
}

func (h *APIHandler) CreateCodeElement(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Project ID is required"})
		return
	}

	var element models.CodeElement
	if err := json.NewDecoder(r.Body).Decode(&element); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Invalid request body"})
		return
	}

	element.ProjectID = projectID

	err := h.postgresStorage.CreateCodeElement(r.Context(), &element)
	if err != nil {
		h.logger.Error("Failed to create code element", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to create code element"})
		return
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, element)
}

func (h *APIHandler) GetCodeElement(w http.ResponseWriter, r *http.Request) {
	elementID := chi.URLParam(r, "elementId")
	if elementID == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Element ID is required"})
		return
	}

	element, err := h.postgresStorage.GetCodeElement(r.Context(), elementID)
	if err != nil {
		h.logger.Error("Failed to get code element", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to get code element"})
		return
	}

	if element == nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": "Code element not found"})
		return
	}

	render.JSON(w, r, element)
}

func (h *APIHandler) UpdateCodeElement(w http.ResponseWriter, r *http.Request) {
	elementID := chi.URLParam(r, "elementId")
	if elementID == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Element ID is required"})
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Invalid request body"})
		return
	}

	element, err := h.postgresStorage.UpdateCodeElement(r.Context(), elementID, updates)
	if err != nil {
		h.logger.Error("Failed to update code element", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to update code element"})
		return
	}

	render.JSON(w, r, element)
}

func (h *APIHandler) DeleteCodeElement(w http.ResponseWriter, r *http.Request) {
	elementID := chi.URLParam(r, "elementId")
	if elementID == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Element ID is required"})
		return
	}

	err := h.postgresStorage.DeleteCodeElement(r.Context(), elementID)
	if err != nil {
		h.logger.Error("Failed to delete code element", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to delete code element"})
		return
	}

	render.Status(r, http.StatusNoContent)
	render.JSON(w, r, map[string]string{"message": "Code element deleted successfully"})
}

func (h *APIHandler) SearchSemantic(w http.ResponseWriter, r *http.Request) {
	var req search.SearchOptions
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Invalid request body"})
		return
	}

	if req.Query == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Query is required"})
		return
	}

	results, err := h.searchService.Search(r.Context(), req)
	if err != nil {
		h.logger.Error("Failed to perform search", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to perform search"})
		return
	}

	render.JSON(w, r, results)
}

func (h *APIHandler) GetKnowledgeGraph(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Project ID is required"})
		return
	}

	options := knowledgegraph.GraphOptions{
		ProjectID: projectID,
		Depth:     2,
		MaxNodes:  100,
	}

	if depthStr := r.URL.Query().Get("depth"); depthStr != "" {
		if depth, err := strconv.Atoi(depthStr); err == nil {
			options.Depth = depth
		}
	}

	if elementTypes := r.URL.Query().Get("elementTypes"); elementTypes != "" {
		options.ElementTypes = parseCommaSeparated(elementTypes)
	}

	if relationTypes := r.URL.Query().Get("relationTypes"); relationTypes != "" {
		options.RelationTypes = parseCommaSeparated(relationTypes)
	}

	graph, err := h.knowledgeGraphService.GetProjectGraph(r.Context(), options)
	if err != nil {
		h.logger.Error("Failed to get knowledge graph", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to get knowledge graph"})
		return
	}

	render.JSON(w, r, graph)
}

func (h *APIHandler) GetElementConnections(w http.ResponseWriter, r *http.Request) {
	elementID := chi.URLParam(r, "elementId")
	if elementID == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Element ID is required"})
		return
	}

	depth := 2
	if depthStr := r.URL.Query().Get("depth"); depthStr != "" {
		if d, err := strconv.Atoi(depthStr); err == nil {
			depth = d
		}
	}

	connections, err := h.knowledgeGraphService.GetElementConnections(r.Context(), elementID, depth)
	if err != nil {
		h.logger.Error("Failed to get element connections", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to get element connections"})
		return
	}

	render.JSON(w, r, connections)
}

func (h *APIHandler) StartAnalysis(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Project ID is required"})
		return
	}

	job := &models.AnalysisJob{
		ProjectID: projectID,
		Status:    "pending",
		Progress:  0,
	}

	err := h.postgresStorage.CreateAnalysisJob(r.Context(), job)
	if err != nil {
		h.logger.Error("Failed to create analysis job", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to create analysis job"})
		return
	}

	queueJob := queue.CreateAnalyzeProjectJob(projectID)
	err = h.queue.Enqueue(r.Context(), queueJob.Type, queueJob.Data)
	if err != nil {
		h.logger.Error("Failed to queue analysis job", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to queue analysis job"})
		return
	}

	render.Status(r, http.StatusAccepted)
	render.JSON(w, r, job)
}

func (h *APIHandler) GetAnalysisJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	if jobID == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Job ID is required"})
		return
	}

	job, err := h.postgresStorage.GetAnalysisJob(r.Context(), jobID)
	if err != nil {
		h.logger.Error("Failed to get analysis job", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to get analysis job"})
		return
	}

	if job == nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": "Analysis job not found"})
		return
	}

	render.JSON(w, r, job)
}

func (h *APIHandler) ListAnalysisJobs(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")

	jobs, err := h.postgresStorage.ListAnalysisJobs(r.Context(), projectID)
	if err != nil {
		h.logger.Error("Failed to list analysis jobs", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to list analysis jobs"})
		return
	}

	render.JSON(w, r, jobs)
}

func (h *APIHandler) GetProjectStats(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Project ID is required"})
		return
	}

	stats, err := h.postgresStorage.GetProjectStats(r.Context(), projectID)
	if err != nil {
		h.logger.Error("Failed to get project stats", zap.Error(err))
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "Failed to get project stats"})
		return
	}

	render.JSON(w, r, stats)
}

func (h *APIHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().UTC(),
		"services": map[string]bool{
			"postgres":  true,
			"neo4j":     true,
			"redis":     true,
			"embedding": h.embeddingService.IsAvailable(r.Context()),
		},
	}

	render.JSON(w, r, health)
}

func parseCommaSeparated(value string) []string {
	if value == "" {
		return []string{}
	}

	parts := make([]string, 0)
	for _, part := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}

	return parts
}
