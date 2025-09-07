package models

import (
	"time"
)

type ElementType string

const (
	Function  ElementType = "function"
	Method    ElementType = "method"
	Struct    ElementType = "struct"
	Interface ElementType = "interface"
	Variable  ElementType = "variable"
	Constant  ElementType = "constant"
	Import    ElementType = "import"
	Comment   ElementType = "comment"
)

type RelationType string

const (
	Calls       RelationType = "CALLS"
	Implements  RelationType = "IMPLEMENTS"
	Uses        RelationType = "USES"
	Contains    RelationType = "CONTAINS"
	Imports     RelationType = "IMPORTS"
	Inherits    RelationType = "INHERITS"
	References  RelationType = "REFERENCES"
	DefinesType RelationType = "DEFINES_TYPE"
	HasMethod   RelationType = "HAS_METHOD"
	HasField    RelationType = "HAS_FIELD"
)

type CodeElement struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Type       ElementType            `json:"type"`
	FilePath   string                 `json:"file_path"`
	StartLine  int                    `json:"start_line"`
	EndLine    int                    `json:"end_line"`
	Package    string                 `json:"package"`
	Signature  string                 `json:"signature"`
	DocComment string                 `json:"doc_comment"`
	Code       string                 `json:"code"`
	Embedding  []float32              `json:"embedding"`
	Metadata   map[string]interface{} `json:"metadata"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

type Relationship struct {
	ID         string                 `json:"id"`
	FromID     string                 `json:"from_id"`
	ToID       string                 `json:"to_id"`
	Type       RelationType           `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	CreatedAt  time.Time              `json:"created_at"`
}

type SearchQuery struct {
	Query           string                 `json:"query"`
	Filters         map[string]interface{} `json:"filters"`
	Limit           int                    `json:"limit"`
	Threshold       float64                `json:"threshold"`
	IncludeMetadata bool                   `json:"include_metadata"`
}

type SearchResult struct {
	Element    CodeElement `json:"element"`
	Score      float64     `json:"score"`
	Similarity float64     `json:"similarity"`
	Matches    []string    `json:"matches"`
}

type Project struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Path       string        `json:"path"`
	Language   string        `json:"language"`
	Elements   []CodeElement `json:"elements"`
	Statistics ProjectStats  `json:"statistics"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

type ProjectStats struct {
	TotalFiles     int                 `json:"total_files"`
	TotalLines     int                 `json:"total_lines"`
	TotalFunctions int                 `json:"total_functions"`
	TotalStructs   int                 `json:"total_structs"`
	ElementCounts  map[ElementType]int `json:"element_counts"`
	PackageCounts  map[string]int      `json:"package_counts"`
}

type GraphNode struct {
	ID         string                 `json:"id"`
	Labels     []string               `json:"labels"`
	Properties map[string]interface{} `json:"properties"`
}

type GraphEdge struct {
	ID         string                 `json:"id"`
	From       string                 `json:"from"`
	To         string                 `json:"to"`
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
}

// background analysis job
type AnalysisJob struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Status    string    `json:"status"`
	Progress  int       `json:"progress"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
