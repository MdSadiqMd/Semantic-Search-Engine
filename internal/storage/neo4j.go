package storage

import (
	"context"
	"fmt"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/models"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type Neo4jStorage struct {
	driver neo4j.DriverWithContext
}

func NewNeo4jStorage(uri, username, password string) (*Neo4jStorage, error) {
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		return nil, fmt.Errorf("failed to create neo4j driver: %w", err)
	}

	storage := &Neo4jStorage{driver: driver}
	if err := storage.initConstraints(); err != nil {
		return nil, fmt.Errorf("failed to initialize constraints: %w", err)
	}
	return storage, nil
}

func (s *Neo4jStorage) initConstraints() error {
	ctx := context.Background()
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	// Create constraints and indexes
	constraints := []string{
		"CREATE CONSTRAINT code_element_id IF NOT EXISTS FOR (e:CodeElement) REQUIRE e.id IS UNIQUE",
		"CREATE CONSTRAINT project_id IF NOT EXISTS FOR (p:Project) REQUIRE p.id IS UNIQUE",
		"CREATE INDEX code_element_type IF NOT EXISTS FOR (e:CodeElement) ON (e.type)",
		"CREATE INDEX code_element_package IF NOT EXISTS FOR (e:CodeElement) ON (e.package)",
		"CREATE INDEX code_element_name IF NOT EXISTS FOR (e:CodeElement) ON (e.name)",
	}

	for _, constraint := range constraints {
		_, err := session.Run(ctx, constraint, nil)
		if err != nil {
			return fmt.Errorf("failed to create constraint: %w", err)
		}
	}

	return nil
}

func (s *Neo4jStorage) CreateCodeElementNode(ctx context.Context, element *models.CodeElement) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	query := `
		MERGE (e:CodeElement {id: $id})
		SET e.name = $name,
			e.type = $type,
			e.file_path = $file_path,
			e.start_line = $start_line,
			e.end_line = $end_line,
			e.package = $package,
			e.signature = $signature,
			e.doc_comment = $doc_comment,
			e.code = $code,
			e.created_at = $created_at,
			e.updated_at = $updated_at
		RETURN e
	`

	params := map[string]interface{}{
		"id":          element.ID,
		"name":        element.Name,
		"type":        string(element.Type),
		"file_path":   element.FilePath,
		"start_line":  element.StartLine,
		"end_line":    element.EndLine,
		"package":     element.Package,
		"signature":   element.Signature,
		"doc_comment": element.DocComment,
		"code":        element.Code,
		"created_at":  element.CreatedAt.Unix(),
		"updated_at":  element.UpdatedAt.Unix(),
	}

	_, err := session.Run(ctx, query, params)
	if err != nil {
		return fmt.Errorf("failed to create code element node: %w", err)
	}

	return nil
}

func (s *Neo4jStorage) CreateRelationshipEdge(ctx context.Context, relationship *models.Relationship) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	query := `
		MATCH (from:CodeElement {id: $from_id})
		MATCH (to:CodeElement {id: $to_id})
		MERGE (from)-[r:%s]->(to)
		SET r.properties = $properties,
			r.created_at = $created_at
		RETURN r
	`
	formattedQuery := fmt.Sprintf(query, string(relationship.Type))

	params := map[string]interface{}{
		"from_id":    relationship.FromID,
		"to_id":      relationship.ToID,
		"properties": relationship.Properties,
		"created_at": relationship.CreatedAt.Unix(),
	}

	_, err := session.Run(ctx, formattedQuery, params)
	if err != nil {
		return fmt.Errorf("failed to create relationship edge: %w", err)
	}

	return nil
}

func (s *Neo4jStorage) GetKnowledgeGraph(ctx context.Context, projectID string, elementTypes []string, relationTypes []string) ([]models.GraphNode, []models.GraphEdge, error) {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	typeFilter := ""
	if len(elementTypes) > 0 {
		typeFilter = "WHERE e.type IN $element_types"
	}

	query := fmt.Sprintf(`
		MATCH (e:CodeElement)
		%s
		OPTIONAL MATCH (e)-[r]->(e2:CodeElement)
		RETURN e, r, e2
		LIMIT 100
	`, typeFilter)

	params := map[string]interface{}{}
	if len(elementTypes) > 0 {
		params["element_types"] = elementTypes
	}

	result, err := session.Run(ctx, query, params)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get knowledge graph: %w", err)
	}

	nodeMap := make(map[string]models.GraphNode)
	var edges []models.GraphEdge

	for result.Next(ctx) {
		record := result.Record()

		// Process node
		if nodeValue, ok := record.Get("e"); ok && nodeValue != nil {
			if node, ok := nodeValue.(neo4j.Node); ok {
				props := node.Props
				id := props["id"].(string)

				nodeMap[id] = models.GraphNode{
					ID:         id,
					Labels:     node.Labels,
					Properties: props,
				}
			}
		}

		// Process relationship
		if relValue, ok := record.Get("r"); ok && relValue != nil {
			if rel, ok := relValue.(neo4j.Relationship); ok {
				props := rel.Props
				if props == nil {
					props = make(map[string]interface{})
				}

				edges = append(edges, models.GraphEdge{
					ID:         fmt.Sprintf("%d", rel.Id),
					From:       fmt.Sprintf("%d", rel.StartId),
					To:         fmt.Sprintf("%d", rel.EndId),
					Type:       rel.Type,
					Properties: props,
				})
			}
		}

		// Process target node
		if nodeValue, ok := record.Get("e2"); ok && nodeValue != nil {
			if node, ok := nodeValue.(neo4j.Node); ok {
				props := node.Props
				id := props["id"].(string)

				nodeMap[id] = models.GraphNode{
					ID:         id,
					Labels:     node.Labels,
					Properties: props,
				}
			}
		}
	}

	// Convert map to slice
	nodes := make([]models.GraphNode, 0, len(nodeMap))
	for _, node := range nodeMap {
		nodes = append(nodes, node)
	}
	return nodes, edges, nil
}

func (s *Neo4jStorage) FindConnectedElements(ctx context.Context, elementID string, depth int) ([]models.GraphNode, []models.GraphEdge, error) {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	query := fmt.Sprintf(`
		MATCH path = (start:CodeElement {id: $element_id})-[*1..%d]-(connected:CodeElement)
		UNWIND relationships(path) as r
		RETURN start, connected, r
	`, depth)

	params := map[string]interface{}{
		"element_id": elementID,
	}

	result, err := session.Run(ctx, query, params)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find connected elements: %w", err)
	}

	nodeMap := make(map[string]models.GraphNode)
	var edges []models.GraphEdge

	for result.Next(ctx) {
		record := result.Record()

		// Process nodes
		for _, key := range []string{"start", "connected"} {
			if nodeValue, ok := record.Get(key); ok && nodeValue != nil {
				if node, ok := nodeValue.(neo4j.Node); ok {
					props := node.Props
					id := props["id"].(string)

					nodeMap[id] = models.GraphNode{
						ID:         id,
						Labels:     node.Labels,
						Properties: props,
					}
				}
			}
		}

		// Process relationship
		if relValue, ok := record.Get("r"); ok && relValue != nil {
			if rel, ok := relValue.(neo4j.Relationship); ok {
				props := rel.Props
				if props == nil {
					props = make(map[string]interface{})
				}

				edges = append(edges, models.GraphEdge{
					ID:         fmt.Sprintf("%d", rel.Id),
					From:       fmt.Sprintf("%d", rel.StartId),
					To:         fmt.Sprintf("%d", rel.EndId),
					Type:       rel.Type,
					Properties: props,
				})
			}
		}
	}

	// Convert map to slice
	nodes := make([]models.GraphNode, 0, len(nodeMap))
	for _, node := range nodeMap {
		nodes = append(nodes, node)
	}
	return nodes, edges, nil
}

func (s *Neo4jStorage) Close() error {
	ctx := context.Background()
	return s.driver.Close(ctx)
}
