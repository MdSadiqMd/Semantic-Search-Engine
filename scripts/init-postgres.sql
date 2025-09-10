-- Enable necessary extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS vector;

-- Create projects table
CREATE TABLE IF NOT EXISTS projects (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    path TEXT NOT NULL,
    language VARCHAR(50) NOT NULL,
    statistics JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create code_elements table with vector column for embeddings
CREATE TABLE IF NOT EXISTS code_elements (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL,
    file_path TEXT NOT NULL,
    start_line INTEGER NOT NULL,
    end_line INTEGER NOT NULL,
    package VARCHAR(255),
    signature TEXT,
    doc_comment TEXT,
    code TEXT NOT NULL,
    embedding vector(768),
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create relationships table
CREATE TABLE IF NOT EXISTS relationships (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    from_id UUID NOT NULL REFERENCES code_elements(id) ON DELETE CASCADE,
    to_id UUID NOT NULL REFERENCES code_elements(id) ON DELETE CASCADE,
    type VARCHAR(50) NOT NULL,
    properties JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create analysis_jobs table
CREATE TABLE IF NOT EXISTS analysis_jobs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    progress INTEGER DEFAULT 0,
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create search_queries table for caching and analytics
CREATE TABLE IF NOT EXISTS search_queries (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    query_text TEXT NOT NULL,
    filters JSONB,
    results_count INTEGER,
    execution_time_ms INTEGER,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create indexes for performance

-- Project indexes
CREATE INDEX IF NOT EXISTS idx_projects_name ON projects(name);
CREATE INDEX IF NOT EXISTS idx_projects_language ON projects(language);
CREATE INDEX IF NOT EXISTS idx_projects_created_at ON projects(created_at);

-- Code elements indexes
CREATE INDEX IF NOT EXISTS idx_code_elements_project_id ON code_elements(project_id);
CREATE INDEX IF NOT EXISTS idx_code_elements_type ON code_elements(type);
CREATE INDEX IF NOT EXISTS idx_code_elements_name ON code_elements(name);
CREATE INDEX IF NOT EXISTS idx_code_elements_package ON code_elements(package);
CREATE INDEX IF NOT EXISTS idx_code_elements_file_path ON code_elements(file_path);

-- Full-text search index on code content
CREATE INDEX IF NOT EXISTS idx_code_elements_search ON code_elements 
USING gin(to_tsvector('english', name || ' ' || COALESCE(doc_comment, '') || ' ' || COALESCE(signature, '')));

-- Vector similarity index (HNSW for better performance with large datasets)
CREATE INDEX IF NOT EXISTS idx_code_elements_embedding ON code_elements 
USING hnsw (embedding vector_cosine_ops) 
WITH (m = 16, ef_construction = 64);

-- Alternative IVFFlat index (faster build, good for smaller datasets)
-- CREATE INDEX IF NOT EXISTS idx_code_elements_embedding_ivf ON code_elements 
-- USING ivfflat (embedding vector_cosine_ops) 
-- WITH (lists = 100);

-- Relationships indexes
CREATE INDEX IF NOT EXISTS idx_relationships_from_id ON relationships(from_id);
CREATE INDEX IF NOT EXISTS idx_relationships_to_id ON relationships(to_id);
CREATE INDEX IF NOT EXISTS idx_relationships_type ON relationships(type);

-- Composite index for bidirectional relationship queries
CREATE INDEX IF NOT EXISTS idx_relationships_bidirectional ON relationships(from_id, to_id);

-- Analysis jobs indexes
CREATE INDEX IF NOT EXISTS idx_analysis_jobs_project_id ON analysis_jobs(project_id);
CREATE INDEX IF NOT EXISTS idx_analysis_jobs_status ON analysis_jobs(status);
CREATE INDEX IF NOT EXISTS idx_analysis_jobs_created_at ON analysis_jobs(created_at);

-- Search queries indexes
CREATE INDEX IF NOT EXISTS idx_search_queries_project_id ON search_queries(project_id);
CREATE INDEX IF NOT EXISTS idx_search_queries_created_at ON search_queries(created_at);

-- Create updated_at trigger function
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Create triggers for updated_at columns
CREATE TRIGGER update_projects_updated_at 
    BEFORE UPDATE ON projects 
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_code_elements_updated_at 
    BEFORE UPDATE ON code_elements 
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_analysis_jobs_updated_at 
    BEFORE UPDATE ON analysis_jobs 
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Create useful views

-- Project summary view
CREATE OR REPLACE VIEW project_summary AS
SELECT 
    p.id,
    p.name,
    p.language,
    p.created_at,
    COUNT(ce.id) as total_elements,
    COUNT(CASE WHEN ce.type = 'function' THEN 1 END) as function_count,
    COUNT(CASE WHEN ce.type = 'struct' THEN 1 END) as struct_count,
    COUNT(CASE WHEN ce.type = 'interface' THEN 1 END) as interface_count,
    COUNT(CASE WHEN ce.type = 'method' THEN 1 END) as method_count,
    COUNT(DISTINCT ce.package) as package_count,
    COUNT(DISTINCT ce.file_path) as file_count,
    COALESCE(SUM(ce.end_line - ce.start_line + 1), 0) as total_lines
FROM projects p
LEFT JOIN code_elements ce ON p.id = ce.project_id
GROUP BY p.id, p.name, p.language, p.created_at;

-- Element relationship view
CREATE OR REPLACE VIEW element_relationships AS
SELECT 
    ce1.name as from_element,
    ce1.type as from_type,
    ce1.package as from_package,
    r.type as relationship_type,
    ce2.name as to_element,
    ce2.type as to_type,
    ce2.package as to_package,
    ce1.project_id
FROM relationships r
JOIN code_elements ce1 ON r.from_id = ce1.id
JOIN code_elements ce2 ON r.to_id = ce2.id;

-- Create utility functions

-- Function to calculate vector similarity
CREATE OR REPLACE FUNCTION calculate_similarity(vec1 vector, vec2 vector)
RETURNS FLOAT AS $$
BEGIN
    RETURN 1 - (vec1 <=> vec2);
END;
$$ LANGUAGE plpgsql;

-- Function to search similar code elements
CREATE OR REPLACE FUNCTION search_similar_elements(
    query_embedding vector(768),
    project_filter UUID DEFAULT NULL,
    type_filter TEXT DEFAULT NULL,
    similarity_threshold FLOAT DEFAULT 0.7,
    result_limit INTEGER DEFAULT 20
)
RETURNS TABLE(
    element_id UUID,
    element_name TEXT,
    element_type TEXT,
    file_path TEXT,
    similarity_score FLOAT
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        ce.id,
        ce.name,
        ce.type,
        ce.file_path,
        (1 - (ce.embedding <=> query_embedding)) as similarity
    FROM code_elements ce
    WHERE ce.embedding IS NOT NULL
    AND (project_filter IS NULL OR ce.project_id = project_filter)
    AND (type_filter IS NULL OR ce.type = type_filter)
    AND (1 - (ce.embedding <=> query_embedding)) >= similarity_threshold
    ORDER BY ce.embedding <=> query_embedding
    LIMIT result_limit;
END;
$$ LANGUAGE plpgsql;

-- Insert some sample data for testing (optional)
INSERT INTO projects (name, path, language, statistics) 
VALUES (
    'sample-go-project',
    '/workspace/sample-go-project',
    'go',
    '{"totalFiles": 10, "totalLines": 1000, "totalFunctions": 50, "totalStructs": 15}'::jsonb
) ON CONFLICT DO NOTHING;

-- Grant permissions
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO postgres;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO postgres;
GRANT ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public TO postgres;

-- Create a read-only user for analytics (optional)
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_user WHERE usename = 'code_discover_readonly') THEN
        CREATE USER code_discover_readonly WITH PASSWORD 'readonly_password';
    END IF;
END
$$;

GRANT CONNECT ON DATABASE vectordb TO code_discover_readonly;
GRANT USAGE ON SCHEMA public TO code_discover_readonly;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO code_discover_readonly;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO code_discover_readonly;

-- Log successful initialization
DO $$
BEGIN
    RAISE NOTICE 'Code Discovery Engine database initialized successfully!';
    RAISE NOTICE 'Tables created: projects, code_elements, relationships, analysis_jobs, search_queries';
    RAISE NOTICE 'Indexes created for performance optimization';
    RAISE NOTICE 'Vector extension enabled for embeddings';
    RAISE NOTICE 'Views and utility functions created';
END
$$;
