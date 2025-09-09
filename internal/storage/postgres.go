package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
)

type PostgresStorage struct {
	pool *pgxpool.Pool
}

func NewPostgresStorage(dsn string) (*PostgresStorage, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database config: %w", err)
	}

	// Register pgvector types
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvec.RegisterTypes(ctx, conn)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	storage := &PostgresStorage{pool: pool}
	if err := storage.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return storage, nil
}

func (s *PostgresStorage) initSchema() error {
	ctx := context.Background()

	// Enable pgvector extension
	_, err := s.pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector")
	if err != nil {
		return fmt.Errorf("failed to enable vector extension: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS projects (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name TEXT NOT NULL,
		path TEXT NOT NULL,
		language TEXT NOT NULL,
		statistics JSONB,
		created_at TIMESTAMP DEFAULT NOW(),
		updated_at TIMESTAMP DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS code_elements (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		file_path TEXT NOT NULL,
		start_line INTEGER NOT NULL,
		end_line INTEGER NOT NULL,
		package TEXT,
		signature TEXT,
		doc_comment TEXT,
		code TEXT NOT NULL,
		embedding vector(768),
		metadata JSONB,
		created_at TIMESTAMP DEFAULT NOW(),
		updated_at TIMESTAMP DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS relationships (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		from_id UUID REFERENCES code_elements(id) ON DELETE CASCADE,
		to_id UUID REFERENCES code_elements(id) ON DELETE CASCADE,
		type TEXT NOT NULL,
		properties JSONB,
		created_at TIMESTAMP DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS analysis_jobs (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
		status TEXT NOT NULL DEFAULT 'pending',
		progress INTEGER DEFAULT 0,
		error TEXT,
		created_at TIMESTAMP DEFAULT NOW(),
		updated_at TIMESTAMP DEFAULT NOW()
	);

	-- Create indexes for performance
	CREATE INDEX IF NOT EXISTS idx_code_elements_project_id ON code_elements(project_id);
	CREATE INDEX IF NOT EXISTS idx_code_elements_type ON code_elements(type);
	CREATE INDEX IF NOT EXISTS idx_code_elements_package ON code_elements(package);
	CREATE INDEX IF NOT EXISTS idx_relationships_from_id ON relationships(from_id);
	CREATE INDEX IF NOT EXISTS idx_relationships_to_id ON relationships(to_id);
	
	-- Vector similarity index (HNSW for better performance)
	CREATE INDEX IF NOT EXISTS idx_code_elements_embedding ON code_elements 
	USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);
	`

	_, err = s.pool.Exec(ctx, schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

func (s *PostgresStorage) CreateProject(ctx context.Context, project *models.Project) error {
	query := `
		INSERT INTO projects (name, path, language, statistics)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at
	`
	err := s.pool.QueryRow(ctx, query, project.Name, project.Path, project.Language, project.Statistics).
		Scan(&project.ID, &project.CreatedAt, &project.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create project: %w", err)
	}

	return nil
}

func (s *PostgresStorage) UpdateProject(ctx context.Context, id string, updates map[string]interface{}) (*models.Project, error) {
	setClauses := []string{}
	args := []interface{}{}
	i := 1

	for k, v := range updates {
		if k == "statistics" {
			b, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal statistics: %w", err)
			}
			v = b
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", k, i))
		args = append(args, v)
		i++
	}

	if len(setClauses) == 0 {
		return nil, fmt.Errorf("no updates provided")
	}

	query := fmt.Sprintf(`UPDATE projects SET %s, updated_at = NOW() WHERE id = $%d RETURNING id, name, path, language, statistics, created_at, updated_at`,
		strings.Join(setClauses, ", "), i)
	args = append(args, id)

	var p models.Project
	var stats []byte
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&p.ID, &p.Name, &p.Path, &p.Language, &stats, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update project: %w", err)
	}

	_ = json.Unmarshal(stats, &p.Statistics)
	return &p, nil
}

func (s *PostgresStorage) DeleteProject(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	return err
}

func (s *PostgresStorage) GetProject(ctx context.Context, id string) (*models.Project, error) {
	query := `
		SELECT id, name, path, language, statistics, created_at, updated_at
		FROM projects WHERE id = $1
	`
	var project models.Project
	err := s.pool.QueryRow(ctx, query, id).
		Scan(&project.ID, &project.Name, &project.Path, &project.Language, &project.Statistics, &project.CreatedAt, &project.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}

	return &project, nil
}

func (s *PostgresStorage) ListProjects(ctx context.Context) ([]models.Project, error) {
	query := `
		SELECT id, name, path, language, statistics, created_at, updated_at
		FROM projects ORDER BY updated_at DESC
	`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list projects: %w", err)
	}
	defer rows.Close()

	var projects []models.Project
	for rows.Next() {
		var project models.Project
		err := rows.Scan(&project.ID, &project.Name, &project.Path, &project.Language, &project.Statistics, &project.CreatedAt, &project.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan project: %w", err)
		}
		projects = append(projects, project)
	}

	return projects, nil
}

func (s *PostgresStorage) GetProjectStats(ctx context.Context, projectID string) (map[string]interface{}, error) {
	var stats []byte
	err := s.pool.QueryRow(ctx, `SELECT statistics FROM projects WHERE id = $1`, projectID).Scan(&stats)
	if err != nil {
		return nil, fmt.Errorf("failed to get project stats: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(stats, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stats: %w", err)
	}

	return result, nil
}

func (s *PostgresStorage) CreateCodeElement(ctx context.Context, element *models.CodeElement) error {
	query := `
		INSERT INTO code_elements (project_id, name, type, file_path, start_line, end_line, package, signature, doc_comment, code, embedding, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, created_at, updated_at
	`
	var embedding pgvector.Vector
	if len(element.Embedding) > 0 {
		embedding = pgvector.NewVector(element.Embedding)
	}

	err := s.pool.QueryRow(ctx, query,
		element.ID, element.Name, element.Type, element.FilePath, element.StartLine, element.EndLine,
		element.Package, element.Signature, element.DocComment, element.Code, embedding, element.Metadata).
		Scan(&element.ID, &element.CreatedAt, &element.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create code element: %w", err)
	}
	return nil
}

func (s *PostgresStorage) GetCodeElement(ctx context.Context, id string) (*models.CodeElement, error) {
	query := `
        SELECT id, project_id, name, type, file_path, start_line, end_line, package, signature, doc_comment, code, embedding, metadata, created_at, updated_at
        FROM code_elements WHERE id = $1
    `
	var element models.CodeElement
	var embedding pgvector.Vector
	var projectID string

	err := s.pool.QueryRow(ctx, query, id).Scan(
		&element.ID, &projectID, &element.Name, &element.Type, &element.FilePath,
		&element.StartLine, &element.EndLine, &element.Package, &element.Signature,
		&element.DocComment, &element.Code, &embedding, &element.Metadata,
		&element.CreatedAt, &element.UpdatedAt)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get code element: %w", err)
	}

	element.ID = projectID
	if embedding.Slice() != nil {
		element.Embedding = make([]float32, len(embedding.Slice()))
		for i, v := range embedding.Slice() {
			element.Embedding[i] = float32(v)
		}
	}

	return &element, nil
}

func (s *PostgresStorage) GetCodeElements(ctx context.Context, filters map[string]interface{}) ([]models.CodeElement, error) {
	query := `
        SELECT id, project_id, name, type, file_path, start_line, end_line, package, signature, doc_comment, code, embedding, metadata, created_at, updated_at
        FROM code_elements WHERE 1=1
    `
	args := []interface{}{}
	argCount := 0

	if projectID, ok := filters["project_id"].(string); ok && projectID != "" {
		argCount++
		query += fmt.Sprintf(" AND project_id = $%d", argCount)
		args = append(args, projectID)
	}

	if elementType, ok := filters["type"].(string); ok && elementType != "" {
		argCount++
		query += fmt.Sprintf(" AND type = $%d", argCount)
		args = append(args, elementType)
	}

	if pkg, ok := filters["package"].(string); ok && pkg != "" {
		argCount++
		query += fmt.Sprintf(" AND package = $%d", argCount)
		args = append(args, pkg)
	}

	if filePath, ok := filters["file_path"].(string); ok && filePath != "" {
		argCount++
		query += fmt.Sprintf(" AND file_path = $%d", argCount)
		args = append(args, filePath)
	}

	if elementTypes, ok := filters["element_types"].([]string); ok && len(elementTypes) > 0 {
		argCount++
		query += fmt.Sprintf(" AND type = ANY($%d)", argCount)
		args = append(args, elementTypes)
	}

	if packages, ok := filters["packages"].([]string); ok && len(packages) > 0 {
		argCount++
		query += fmt.Sprintf(" AND package = ANY($%d)", argCount)
		args = append(args, packages)
	}

	query += " ORDER BY created_at DESC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get code elements: %w", err)
	}
	defer rows.Close()

	var elements []models.CodeElement
	for rows.Next() {
		var element models.CodeElement
		var embedding pgvector.Vector
		var projectID string

		err := rows.Scan(
			&element.ID, &projectID, &element.Name, &element.Type, &element.FilePath,
			&element.StartLine, &element.EndLine, &element.Package, &element.Signature,
			&element.DocComment, &element.Code, &embedding, &element.Metadata,
			&element.CreatedAt, &element.UpdatedAt)

		if err != nil {
			return nil, fmt.Errorf("failed to scan code element: %w", err)
		}

		element.ID = projectID
		if embedding.Slice() != nil {
			element.Embedding = make([]float32, len(embedding.Slice()))
			for i, v := range embedding.Slice() {
				element.Embedding[i] = float32(v)
			}
		}

		elements = append(elements, element)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate code elements: %w", err)
	}

	return elements, nil
}

func (s *PostgresStorage) UpdateCodeElement(ctx context.Context, id string, updates map[string]interface{}) (*models.CodeElement, error) {
	setClauses := []string{}
	args := []interface{}{}
	i := 1

	for k, v := range updates {
		if k == "metadata" {
			b, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal metadata: %w", err)
			}
			v = b
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", k, i))
		args = append(args, v)
		i++
	}

	if len(setClauses) == 0 {
		return nil, fmt.Errorf("no updates provided")
	}

	query := fmt.Sprintf(`UPDATE code_elements SET %s, updated_at = NOW() WHERE id = $%d 
		RETURNING id, project_id, name, type, file_path, start_line, end_line, package, signature, doc_comment, code, metadata, created_at, updated_at`,
		strings.Join(setClauses, ", "), i)
	args = append(args, id)

	var e models.CodeElement
	var metadata []byte
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&e.ID, &e.ProjectID, &e.Name, &e.Type, &e.FilePath, &e.StartLine,
		&e.EndLine, &e.Package, &e.Signature, &e.DocComment, &e.Code,
		&metadata, &e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update code element: %w", err)
	}

	_ = json.Unmarshal(metadata, &e.Metadata)
	return &e, nil
}

func (s *PostgresStorage) DeleteCodeElement(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM code_elements WHERE id = $1`, id)
	return err
}

func (s *PostgresStorage) SearchSimilar(ctx context.Context, embedding []float32, limit int, threshold float64, filters map[string]interface{}) ([]models.SearchResult, error) {
	query := `
		SELECT id, project_id, name, type, file_path, start_line, end_line, package, signature, doc_comment, code, metadata, created_at, updated_at,
		       1 - (embedding <=> $1) as similarity
		FROM code_elements 
		WHERE embedding IS NOT NULL AND (1 - (embedding <=> $1)) >= $2
	`
	args := []interface{}{pgvector.NewVector(embedding), threshold}
	argCount := 2

	// Add filters
	if projectID, ok := filters["project_id"].(string); ok && projectID != "" {
		argCount++
		query += fmt.Sprintf(" AND project_id = $%d", argCount)
		args = append(args, projectID)
	}

	if elementType, ok := filters["type"].(string); ok && elementType != "" {
		argCount++
		query += fmt.Sprintf(" AND type = $%d", argCount)
		args = append(args, elementType)
	}

	if pkg, ok := filters["package"].(string); ok && pkg != "" {
		argCount++
		query += fmt.Sprintf(" AND package = $%d", argCount)
		args = append(args, pkg)
	}

	query += fmt.Sprintf(" ORDER BY similarity DESC LIMIT $%d", argCount+1)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search similar: %w", err)
	}
	defer rows.Close()

	var results []models.SearchResult
	for rows.Next() {
		var element models.CodeElement
		var similarity float64
		err := rows.Scan(
			&element.ID, &element.ID, &element.Name, &element.Type, &element.FilePath,
			&element.StartLine, &element.EndLine, &element.Package, &element.Signature,
			&element.DocComment, &element.Code, &element.Metadata, &element.CreatedAt,
			&element.UpdatedAt, &similarity)
		if err != nil {
			return nil, fmt.Errorf("failed to scan result: %w", err)
		}

		results = append(results, models.SearchResult{
			Element:    element,
			Score:      similarity,
			Similarity: similarity,
			Matches:    []string{}, // TODO: implement match highlighting
		})
	}

	return results, nil
}

func (s *PostgresStorage) CreateRelationship(ctx context.Context, relationship *models.Relationship) error {
	query := `
		INSERT INTO relationships (from_id, to_id, type, properties)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at
	`
	err := s.pool.QueryRow(ctx, query, relationship.FromID, relationship.ToID, relationship.Type, relationship.Properties).
		Scan(&relationship.ID, &relationship.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create relationship: %w", err)
	}

	return nil
}

func (s *PostgresStorage) GetRelationships(ctx context.Context, elementID string) ([]models.Relationship, error) {
	query := `
		SELECT id, from_id, to_id, type, properties, created_at
		FROM relationships 
		WHERE from_id = $1 OR to_id = $1
	`
	rows, err := s.pool.Query(ctx, query, elementID)
	if err != nil {
		return nil, fmt.Errorf("failed to get relationships: %w", err)
	}
	defer rows.Close()

	var relationships []models.Relationship
	for rows.Next() {
		var rel models.Relationship
		err := rows.Scan(&rel.ID, &rel.FromID, &rel.ToID, &rel.Type, &rel.Properties, &rel.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan relationship: %w", err)
		}
		relationships = append(relationships, rel)
	}

	return relationships, nil
}

func (s *PostgresStorage) CreateAnalysisJob(ctx context.Context, job *models.AnalysisJob) error {
	query := `
		INSERT INTO analysis_jobs (project_id, status, progress)
		VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at
	`
	err := s.pool.QueryRow(ctx, query, job.ProjectID, job.Status, job.Progress).
		Scan(&job.ID, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create analysis job: %w", err)
	}

	return nil
}

func (s *PostgresStorage) ListAnalysisJobs(ctx context.Context, projectID string) ([]*models.AnalysisJob, error) {
	query := `SELECT id, project_id, status, progress, error, created_at, updated_at 
	          FROM analysis_jobs`
	args := []interface{}{}

	if projectID != "" {
		query += " WHERE project_id = $1"
		args = append(args, projectID)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list analysis jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*models.AnalysisJob
	for rows.Next() {
		var j models.AnalysisJob
		err := rows.Scan(&j.ID, &j.ProjectID, &j.Status, &j.Progress, &j.Error, &j.CreatedAt, &j.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job: %w", err)
		}
		jobs = append(jobs, &j)
	}

	return jobs, nil
}

func (s *PostgresStorage) UpdateAnalysisJob(ctx context.Context, id string, status string, progress int, error string) error {
	query := `
		UPDATE analysis_jobs 
		SET status = $2, progress = $3, error = $4, updated_at = NOW()
		WHERE id = $1
	`
	_, err := s.pool.Exec(ctx, query, id, status, progress, error)
	if err != nil {
		return fmt.Errorf("failed to update analysis job: %w", err)
	}

	return nil
}

func (s *PostgresStorage) GetAnalysisJob(ctx context.Context, id string) (*models.AnalysisJob, error) {
	query := `
		SELECT id, project_id, status, progress, error, created_at, updated_at
		FROM analysis_jobs WHERE id = $1
	`
	var job models.AnalysisJob
	err := s.pool.QueryRow(ctx, query, id).
		Scan(&job.ID, &job.ProjectID, &job.Status, &job.Progress, &job.Error, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get analysis job: %w", err)
	}

	return &job, nil
}

func (s *PostgresStorage) Close() error {
	if s.pool != nil {
		s.pool.Close()
	}
	return nil
}
