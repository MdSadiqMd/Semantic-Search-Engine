package ast

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/models"
)

type Parser interface {
	ParseFile(ctx context.Context, filePath string) ([]models.CodeElement, []models.Relationship, error)
	GetSupportedExtensions() []string
	GetLanguage() string
}

type ParserRegistry struct {
	parsers map[string]Parser
}

func NewParserRegistry() *ParserRegistry {
	registry := &ParserRegistry{
		parsers: make(map[string]Parser),
	}

	registry.RegisterParser(NewGoParser())

	return registry
}

func (r *ParserRegistry) RegisterParser(parser Parser) {
	for _, ext := range parser.GetSupportedExtensions() {
		r.parsers[ext] = parser
	}
}

func (r *ParserRegistry) GetParser(filePath string) (Parser, error) {
	ext := filepath.Ext(filePath)
	if ext == "" {
		return nil, fmt.Errorf("no file extension found for %s", filePath)
	}

	parser, exists := r.parsers[ext]
	if !exists {
		return nil, fmt.Errorf("no parser found for extension %s", ext)
	}

	return parser, nil
}

func (r *ParserRegistry) GetSupportedExtensions() []string {
	extensions := make([]string, 0, len(r.parsers))
	for ext := range r.parsers {
		extensions = append(extensions, ext)
	}
	return extensions
}

func (r *ParserRegistry) ParseProject(ctx context.Context, projectPath string, ignorePatterns []string) ([]models.CodeElement, []models.Relationship, error) {
	var allElements []models.CodeElement
	var allRelationships []models.Relationship

	err := filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relativePath, _ := filepath.Rel(projectPath, path)
		for _, pattern := range ignorePatterns {
			if matched, _ := filepath.Match(pattern, relativePath); matched {
				return nil
			}
			if strings.Contains(relativePath, pattern) {
				return nil
			}
		}

		parser, err := r.GetParser(path)
		if err != nil {
			return nil
		}

		elements, relationships, err := parser.ParseFile(ctx, path)
		if err != nil {
			fmt.Printf("Failed to parse %s: %v\n", path, err)
			return nil
		}

		for i := range elements {
			elements[i].FilePath = relativePath
		}

		allElements = append(allElements, elements...)
		allRelationships = append(allRelationships, relationships...)

		return nil
	})

	if err != nil {
		return nil, nil, fmt.Errorf("failed to walk project directory: %w", err)
	}

	return allElements, allRelationships, nil
}

func ReadFileContent(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}
	return string(content), nil
}

func GetPackageFromPath(filePath, projectRoot string) string {
	relativePath, err := filepath.Rel(projectRoot, filePath)
	if err != nil {
		return ""
	}

	dir := filepath.Dir(relativePath)
	if dir == "." {
		return "main"
	}

	return strings.ReplaceAll(dir, string(filepath.Separator), "/")
}

func ExtractDocComment(comments []string) string {
	if len(comments) == 0 {
		return ""
	}

	var docLines []string
	for _, comment := range comments {
		cleaned := strings.TrimSpace(comment)
		cleaned = strings.TrimPrefix(cleaned, "//")
		cleaned = strings.TrimPrefix(cleaned, "/*")
		cleaned = strings.TrimSuffix(cleaned, "*/")
		cleaned = strings.TrimPrefix(cleaned, "#")
		cleaned = strings.TrimSpace(cleaned)

		if cleaned != "" {
			docLines = append(docLines, cleaned)
		}
	}

	return strings.Join(docLines, "\n")
}

func GenerateElementID(projectID, filePath string, startLine int, elementType, name string) string {
	return fmt.Sprintf("%s:%s:%d:%s:%s", projectID, filePath, startLine, elementType, name)
}

func CountLines(content string) int {
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}

func GetLineRange(content string, start, end int) string {
	lines := strings.Split(content, "\n")
	if start < 1 || end > len(lines) || start > end {
		return ""
	}

	return strings.Join(lines[start-1:end], "\n")
}
