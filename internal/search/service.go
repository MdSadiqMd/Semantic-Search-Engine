package search

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/embedding"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/models"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/storage"
	"go.uber.org/zap"
)

type Service struct {
	storage          *storage.PostgresStorage
	embeddingService *embedding.Service
	logger           *zap.Logger
}

func NewService(storage *storage.PostgresStorage, embeddingService *embedding.Service, logger *zap.Logger) *Service {
	return &Service{
		storage:          storage,
		embeddingService: embeddingService,
		logger:           logger,
	}
}

type SearchOptions struct {
	ProjectID       string                 `json:"project_id"`
	Query           string                 `json:"query"`
	Filters         map[string]interface{} `json:"filters"`
	Limit           int                    `json:"limit"`
	Offset          int                    `json:"offset"`
	Threshold       float64                `json:"threshold"`
	IncludeMetadata bool                   `json:"include_metadata"`
	ElementTypes    []string               `json:"element_types"`
	Packages        []string               `json:"packages"`
	FileTypes       []string               `json:"file_types"`
}

type SearchResult struct {
	Element    models.CodeElement `json:"element"`
	Score      float64            `json:"score"`
	Similarity float64            `json:"similarity"`
	Matches    []string           `json:"matches"`
	Highlights []Highlight        `json:"highlights"`
}

type Highlight struct {
	Field string `json:"field"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

type SearchResponse struct {
	Results []SearchResult         `json:"results"`
	Total   int                    `json:"total"`
	Query   string                 `json:"query"`
	Took    int64                  `json:"took"` // milliseconds
	Filters map[string]interface{} `json:"filters"`
}

func (s *Service) Search(ctx context.Context, options SearchOptions) (*SearchResponse, error) {
	s.logger.Info("Starting semantic search",
		zap.String("query", options.Query),
		zap.String("project_id", options.ProjectID),
		zap.Int("limit", options.Limit))

	if options.Query == "" {
		return &SearchResponse{
			Results: []SearchResult{},
			Total:   0,
			Query:   options.Query,
		}, nil
	}

	if options.Limit <= 0 {
		options.Limit = 20
	}
	if options.Threshold <= 0 {
		options.Threshold = 0.5
	}

	queryEmbedding, err := s.embeddingService.GenerateEmbedding(ctx, options.Query)
	if err != nil {
		s.logger.Error("Failed to generate query embedding", zap.Error(err))
		return s.fallbackTextSearch(ctx, options)
	}

	dbFilters := make(map[string]interface{})
	if options.ProjectID != "" {
		dbFilters["project_id"] = options.ProjectID
	}
	if len(options.ElementTypes) > 0 {
		dbFilters["element_types"] = options.ElementTypes
	}
	if len(options.Packages) > 0 {
		dbFilters["packages"] = options.Packages
	}

	searchResults, err := s.storage.SearchSimilar(ctx, queryEmbedding, options.Limit+options.Offset, options.Threshold, dbFilters)
	if err != nil {
		s.logger.Error("Vector search failed", zap.Error(err))
		return s.fallbackTextSearch(ctx, options)
	}

	if options.Offset > 0 && options.Offset < len(searchResults) {
		searchResults = searchResults[options.Offset:]
	} else if options.Offset >= len(searchResults) {
		searchResults = []models.SearchResult{}
	}

	if len(searchResults) > options.Limit {
		searchResults = searchResults[:options.Limit]
	}

	results := make([]SearchResult, len(searchResults))
	for i, result := range searchResults {
		highlights := s.generateHighlights(options.Query, result.Element)
		matches := s.extractMatches(options.Query, result.Element)

		results[i] = SearchResult{
			Element:    result.Element,
			Score:      result.Score,
			Similarity: result.Similarity,
			Matches:    matches,
			Highlights: highlights,
		}
	}

	response := &SearchResponse{
		Results: results,
		Total:   len(results),
		Query:   options.Query,
		Filters: options.Filters,
	}

	s.logger.Info("Search completed",
		zap.String("query", options.Query),
		zap.Int("results", len(results)))

	return response, nil
}

func (s *Service) fallbackTextSearch(ctx context.Context, options SearchOptions) (*SearchResponse, error) {
	s.logger.Info("Using fallback text search", zap.String("query", options.Query))

	dbFilters := make(map[string]interface{})
	if options.ProjectID != "" {
		dbFilters["project_id"] = options.ProjectID
	}

	elements, err := s.storage.GetCodeElements(ctx, dbFilters)
	if err != nil {
		return nil, fmt.Errorf("failed to get code elements: %w", err)
	}

	var results []SearchResult
	queryLower := strings.ToLower(options.Query)
	queryTerms := strings.Fields(queryLower)

	for _, element := range elements {
		score := s.calculateTextScore(queryTerms, element)
		if score >= options.Threshold {
			highlights := s.generateHighlights(options.Query, element)
			matches := s.extractMatches(options.Query, element)

			results = append(results, SearchResult{
				Element:    element,
				Score:      score,
				Similarity: score,
				Matches:    matches,
				Highlights: highlights,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	total := len(results)
	start := options.Offset
	end := start + options.Limit

	if start > total {
		results = []SearchResult{}
	} else {
		if end > total {
			end = total
		}
		results = results[start:end]
	}

	return &SearchResponse{
		Results: results,
		Total:   total,
		Query:   options.Query,
		Filters: options.Filters,
	}, nil
}

func (s *Service) calculateTextScore(queryTerms []string, element models.CodeElement) float64 {
	fmt.Printf("%s %s %s %s", element.Name, element.DocComment, element.Code, element.Signature)
	score := 0.0
	termCount := len(queryTerms)

	for _, term := range queryTerms {
		// Name matching (highest weight)
		if strings.Contains(strings.ToLower(element.Name), term) {
			score += 0.8
		}

		// Doc comment matching
		if strings.Contains(strings.ToLower(element.DocComment), term) {
			score += 0.6
		}

		// Signature matching
		if strings.Contains(strings.ToLower(element.Signature), term) {
			score += 0.5
		}

		// Code matching (lowest weight)
		if strings.Contains(strings.ToLower(element.Code), term) {
			score += 0.3
		}
	}

	if termCount > 0 {
		score /= float64(termCount)
	}

	return score
}

func (s *Service) generateHighlights(query string, element models.CodeElement) []Highlight {
	var highlights []Highlight
	queryLower := strings.ToLower(query)
	queryTerms := strings.Fields(queryLower)

	// Check name field
	if nameHighlights := s.findHighlights(queryTerms, element.Name, "name"); len(nameHighlights) > 0 {
		highlights = append(highlights, nameHighlights...)
	}

	// Check doc comment field
	if docHighlights := s.findHighlights(queryTerms, element.DocComment, "doc_comment"); len(docHighlights) > 0 {
		highlights = append(highlights, docHighlights...)
	}

	// Check signature field
	if sigHighlights := s.findHighlights(queryTerms, element.Signature, "signature"); len(sigHighlights) > 0 {
		highlights = append(highlights, sigHighlights...)
	}

	return highlights
}

func (s *Service) findHighlights(queryTerms []string, text, field string) []Highlight {
	var highlights []Highlight
	textLower := strings.ToLower(text)

	for _, term := range queryTerms {
		if term == "" {
			continue
		}

		start := 0
		for {
			index := strings.Index(textLower[start:], term)
			if index == -1 {
				break
			}

			absoluteIndex := start + index
			highlights = append(highlights, Highlight{
				Field: field,
				Start: absoluteIndex,
				End:   absoluteIndex + len(term),
				Text:  text[absoluteIndex : absoluteIndex+len(term)],
			})

			start = absoluteIndex + len(term)
		}
	}

	return highlights
}

func (s *Service) extractMatches(query string, element models.CodeElement) []string {
	var matches []string
	queryLower := strings.ToLower(query)
	queryTerms := strings.Fields(queryLower)

	for _, term := range queryTerms {
		if strings.Contains(strings.ToLower(element.Name), term) {
			matches = append(matches, "name")
		}
		if strings.Contains(strings.ToLower(element.DocComment), term) {
			matches = append(matches, "documentation")
		}
		if strings.Contains(strings.ToLower(element.Signature), term) {
			matches = append(matches, "signature")
		}
		if strings.Contains(strings.ToLower(element.Code), term) {
			matches = append(matches, "code")
		}
	}

	uniqueMatches := make(map[string]bool)
	for _, match := range matches {
		uniqueMatches[match] = true
	}

	result := make([]string, 0, len(uniqueMatches))
	for match := range uniqueMatches {
		result = append(result, match)
	}

	return result
}

func (s *Service) GetSimilarElements(ctx context.Context, elementID string, limit int) ([]SearchResult, error) {
	element, err := s.storage.GetCodeElement(ctx, elementID)
	if err != nil {
		return nil, fmt.Errorf("failed to get element: %w", err)
	}

	if element == nil {
		return nil, fmt.Errorf("element not found")
	}

	if len(element.Embedding) == 0 {
		return nil, fmt.Errorf("element has no embedding")
	}

	filters := map[string]interface{}{
		"project_id": element.ProjectID,
	}

	searchResults, err := s.storage.SearchSimilar(ctx, element.Embedding, limit+1, 0.1, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to search similar: %w", err)
	}

	var results []SearchResult
	for _, result := range searchResults {
		if result.Element.ID != elementID {
			results = append(results, SearchResult{
				Element:    result.Element,
				Score:      result.Score,
				Similarity: result.Similarity,
				Matches:    []string{"embedding"},
			})
		}

		if len(results) >= limit {
			break
		}
	}

	return results, nil
}

func (s *Service) SearchSuggestions(ctx context.Context, query string, projectID string, limit int) ([]string, error) {
	if len(query) < 2 {
		return []string{}, nil
	}

	dbFilters := make(map[string]interface{})
	if projectID != "" {
		dbFilters["project_id"] = projectID
	}

	elements, err := s.storage.GetCodeElements(ctx, dbFilters)
	if err != nil {
		return nil, fmt.Errorf("failed to get elements: %w", err)
	}

	// Collect suggestions from element names and common terms
	suggestions := make(map[string]bool)
	queryLower := strings.ToLower(query)

	for _, element := range elements {
		name := strings.ToLower(element.Name)
		if strings.Contains(name, queryLower) {
			suggestions[element.Name] = true
		}

		// Add common terms from doc comments
		words := strings.Fields(element.DocComment)
		for _, word := range words {
			wordLower := strings.ToLower(word)
			if len(wordLower) > 3 && strings.Contains(wordLower, queryLower) {
				suggestions[word] = true
			}
		}

		if len(suggestions) >= limit*2 {
			break
		}
	}

	// Convert to slice and limit
	result := make([]string, 0, len(suggestions))
	for suggestion := range suggestions {
		result = append(result, suggestion)
		if len(result) >= limit {
			break
		}
	}

	sort.Strings(result)
	return result, nil
}
