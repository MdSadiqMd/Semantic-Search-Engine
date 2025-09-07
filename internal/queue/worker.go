package queue

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

type JobHandler func(ctx context.Context, job *Job) error

type Worker struct {
	queue    *RedisQueue
	handlers map[string]JobHandler
	logger   *zap.Logger
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewWorker(queue *RedisQueue, logger *zap.Logger) *Worker {
	ctx, cancel := context.WithCancel(context.Background())

	return &Worker{
		queue:    queue,
		handlers: make(map[string]JobHandler),
		logger:   logger,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (w *Worker) RegisterHandler(jobType string, handler JobHandler) {
	w.handlers[jobType] = handler
}

func (w *Worker) Start() {
	w.logger.Info("Starting worker")

	for {
		select {
		case <-w.ctx.Done():
			w.logger.Info("Worker stopped")
			return
		default:
			job, err := w.queue.Pop(w.ctx, 5*time.Second)
			if err != nil {
				w.logger.Error("Failed to pop job", zap.Error(err))
				continue
			}

			if job == nil {
				continue // No job available, continue polling
			}

			w.processJob(job)
		}
	}
}

func (w *Worker) processJob(job *Job) {
	w.logger.Info("Processing job",
		zap.String("id", job.ID),
		zap.String("type", job.Type),
		zap.Int("attempts", job.Attempts))

	handler, exists := w.handlers[job.Type]
	if !exists {
		w.logger.Error("No handler found for job type", zap.String("type", job.Type))
		return
	}

	job.Attempts++

	err := handler(w.ctx, job)
	if err != nil {
		w.logger.Error("Job failed",
			zap.String("id", job.ID),
			zap.String("type", job.Type),
			zap.Error(err),
			zap.Int("attempts", job.Attempts))

		if job.Attempts < job.MaxRetries {
			w.logger.Info("Retrying job",
				zap.String("id", job.ID),
				zap.Int("attempts", job.Attempts))

			// exponential backoff
			time.Sleep(time.Duration(job.Attempts*job.Attempts) * time.Second)

			if pushErr := w.queue.Push(w.ctx, job); pushErr != nil {
				w.logger.Error("Failed to requeue job", zap.Error(pushErr))
			}
		} else {
			w.logger.Error("Job failed permanently",
				zap.String("id", job.ID),
				zap.String("type", job.Type))
		}
	} else {
		w.logger.Info("Job completed successfully",
			zap.String("id", job.ID),
			zap.String("type", job.Type))
	}
}

func (w *Worker) Stop() {
	w.logger.Info("Stopping worker")
	w.cancel()
}

const (
	JobTypeAnalyzeProject      = "analyze_project"
	JobTypeGenerateEmbedding   = "generate_embedding"
	JobTypeExtractAST          = "extract_ast"
	JobTypeBuildKnowledgeGraph = "build_knowledge_graph"
)

func CreateAnalyzeProjectJob(projectID string) *Job {
	return &Job{
		ID:         fmt.Sprintf("analyze_%s_%d", projectID, time.Now().Unix()),
		Type:       JobTypeAnalyzeProject,
		Data:       map[string]interface{}{"project_id": projectID},
		CreatedAt:  time.Now(),
		Attempts:   0,
		MaxRetries: 3,
	}
}

func CreateGenerateEmbeddingJob(elementID string, text string) *Job {
	return &Job{
		ID:   fmt.Sprintf("embedding_%s_%d", elementID, time.Now().Unix()),
		Type: JobTypeGenerateEmbedding,
		Data: map[string]interface{}{
			"element_id": elementID,
			"text":       text,
		},
		CreatedAt:  time.Now(),
		Attempts:   0,
		MaxRetries: 3,
	}
}

func CreateExtractASTJob(projectID, filePath string) *Job {
	return &Job{
		ID:   fmt.Sprintf("ast_%s_%d", projectID, time.Now().Unix()),
		Type: JobTypeExtractAST,
		Data: map[string]interface{}{
			"project_id": projectID,
			"file_path":  filePath,
		},
		CreatedAt:  time.Now(),
		Attempts:   0,
		MaxRetries: 3,
	}
}
