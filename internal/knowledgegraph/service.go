package knowledgegraph

import (
	"context"
	"fmt"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/models"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/storage"
	"go.uber.org/zap"
)

type Service struct {
	neo4jStorage    *storage.Neo4jStorage
	postgresStorage *storage.PostgresStorage
	logger          *zap.Logger
}

func NewService(neo4jStorage *storage.Neo4jStorage, postgresStorage *storage.PostgresStorage, logger *zap.Logger) *Service {
	return &Service{
		neo4jStorage:    neo4jStorage,
		postgresStorage: postgresStorage,
		logger:          logger,
	}
}

type GraphOptions struct {
	ProjectID     string   `json:"project_id"`
	ElementTypes  []string `json:"element_types"`
	RelationTypes []string `json:"relation_types"`
	Depth         int      `json:"depth"`
	MaxNodes      int      `json:"max_nodes"`
	IncludeCode   bool     `json:"include_code"`
}

type GraphAnalytics struct {
	NodeCount         int                    `json:"node_count"`
	EdgeCount         int                    `json:"edge_count"`
	ComponentCount    int                    `json:"component_count"`
	MaxDepth          int                    `json:"max_depth"`
	TypeDistribution  map[string]int         `json:"type_distribution"`
	CentralityMetrics map[string]interface{} `json:"centrality_metrics"`
}

type KnowledgeGraphData struct {
	Nodes []models.GraphNode
	Edges []models.GraphEdge
}

func (s *Service) GetProjectGraph(ctx context.Context, options GraphOptions) (*KnowledgeGraphData, error) {
	s.logger.Info("Getting knowledge graph",
		zap.String("project_id", options.ProjectID),
		zap.Strings("element_types", options.ElementTypes),
		zap.Int("depth", options.Depth))

	if options.Depth <= 0 {
		options.Depth = 2
	}
	if options.MaxNodes <= 0 {
		options.MaxNodes = 100
	}

	nodes, edges, err := s.neo4jStorage.GetKnowledgeGraph(ctx, options.ProjectID, options.ElementTypes, options.RelationTypes)
	if err != nil {
		s.logger.Error("Failed to get graph from Neo4j", zap.Error(err))
		return s.generateGraphFromPostgres(ctx, options)
	}

	if len(nodes) > options.MaxNodes {
		nodes, edges = s.limitGraph(nodes, edges, options.MaxNodes)
	}

	if options.IncludeCode {
		err = s.enrichNodesWithCode(ctx, nodes)
		if err != nil {
			s.logger.Warn("Failed to enrich nodes with code", zap.Error(err))
		}
	}

	return &KnowledgeGraphData{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

func (s *Service) GetElementConnections(ctx context.Context, elementID string, depth int) (*KnowledgeGraphData, error) {
	s.logger.Info("Getting element connections",
		zap.String("element_id", elementID),
		zap.Int("depth", depth))

	if depth <= 0 {
		depth = 2
	}

	nodes, edges, err := s.neo4jStorage.FindConnectedElements(ctx, elementID, depth)
	if err != nil {
		s.logger.Error("Failed to get connections from Neo4j", zap.Error(err))
		return s.generateConnectionsFromPostgres(ctx, elementID, depth)
	}

	return &KnowledgeGraphData{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

func (s *Service) BuildGraph(ctx context.Context, projectID string) error {
	s.logger.Info("Building knowledge graph", zap.String("project_id", projectID))
	filters := map[string]interface{}{
		"project_id": projectID,
	}

	elements, err := s.postgresStorage.GetCodeElements(ctx, filters)
	if err != nil {
		return fmt.Errorf("failed to get code elements: %w", err)
	}

	s.logger.Info("Creating nodes in Neo4j", zap.Int("count", len(elements)))
	for _, element := range elements {
		err := s.neo4jStorage.CreateCodeElementNode(ctx, &element)
		if err != nil {
			s.logger.Error("Failed to create node",
				zap.String("element_id", element.ID),
				zap.Error(err))
		}
	}

	relationships, err := s.postgresStorage.GetRelationships(ctx, projectID)
	if err != nil {
		return fmt.Errorf("failed to get relationships: %w", err)
	}
	s.logger.Info("Creating relationships in Neo4j", zap.Int("count", len(relationships)))

	for _, relationship := range relationships {
		err := s.neo4jStorage.CreateRelationshipEdge(ctx, &relationship)
		if err != nil {
			s.logger.Error("Failed to create relationship",
				zap.String("relationship_id", relationship.ID),
				zap.Error(err))
		}
	}

	s.logger.Info("Knowledge graph build completed", zap.String("project_id", projectID))
	return nil
}

func (s *Service) AnalyzeGraph(ctx context.Context, projectID string) (*GraphAnalytics, error) {
	s.logger.Info("Analyzing knowledge graph", zap.String("project_id", projectID))
	options := GraphOptions{
		ProjectID: projectID,
		Depth:     3,
		MaxNodes:  500,
	}

	graphData, err := s.GetProjectGraph(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("failed to get graph: %w", err)
	}

	analytics := &GraphAnalytics{
		NodeCount:        len(graphData.Nodes),
		EdgeCount:        len(graphData.Edges),
		TypeDistribution: make(map[string]int),
	}

	for _, node := range graphData.Nodes {
		if nodeType, ok := node.Properties["type"].(string); ok {
			analytics.TypeDistribution[nodeType]++
		}
	}

	analytics.ComponentCount = s.calculateConnectedComponents(graphData)
	analytics.CentralityMetrics = s.calculateCentralityMetrics(graphData)
	analytics.MaxDepth = s.calculateMaxDepth(graphData)
	return analytics, nil
}

func (s *Service) FindShortestPath(ctx context.Context, fromElementID, toElementID string) ([]models.GraphNode, []models.GraphEdge, error) {
	s.logger.Info("Finding shortest path",
		zap.String("from", fromElementID),
		zap.String("to", toElementID))

	fromGraph, err := s.GetElementConnections(ctx, fromElementID, 3)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get from connections: %w", err)
	}

	toGraph, err := s.GetElementConnections(ctx, toElementID, 3)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get to connections: %w", err)
	}

	allNodes := append(fromGraph.Nodes, toGraph.Nodes...)
	allEdges := append(fromGraph.Edges, toGraph.Edges...)

	path := s.findPathBFS(allNodes, allEdges, fromElementID, toElementID)
	if len(path) == 0 {
		return nil, nil, fmt.Errorf("no path found between elements")
	}

	pathNodes, pathEdges := s.extractPathElements(allNodes, allEdges, path)

	return pathNodes, pathEdges, nil
}

func (s *Service) GetInfluentialElements(ctx context.Context, projectID string, limit int) ([]models.GraphNode, error) {
	s.logger.Info("Finding influential elements",
		zap.String("project_id", projectID),
		zap.Int("limit", limit))

	options := GraphOptions{
		ProjectID: projectID,
		Depth:     2,
		MaxNodes:  200,
	}

	graphData, err := s.GetProjectGraph(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("failed to get graph: %w", err)
	}

	// Calculate node centrality (degree centrality as a simple metric)
	nodeCentrality := make(map[string]int)
	for _, edge := range graphData.Edges {
		nodeCentrality[edge.From]++
		nodeCentrality[edge.To]++
	}

	// Sort nodes by centrality
	type nodeScore struct {
		node  models.GraphNode
		score int
	}

	var scores []nodeScore
	for _, node := range graphData.Nodes {
		score := nodeCentrality[node.ID]
		scores = append(scores, nodeScore{node: node, score: score})
	}

	// Sort by score (descending)
	for i := 0; i < len(scores)-1; i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[i].score < scores[j].score {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}

	// Return top nodes
	if limit > len(scores) {
		limit = len(scores)
	}

	result := make([]models.GraphNode, limit)
	for i := 0; i < limit; i++ {
		result[i] = scores[i].node
	}

	return result, nil
}

func (s *Service) generateGraphFromPostgres(ctx context.Context, options GraphOptions) (*KnowledgeGraphData, error) {
	filters := map[string]interface{}{
		"project_id": options.ProjectID,
	}

	elements, err := s.postgresStorage.GetCodeElements(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get elements: %w", err)
	}

	// Convert elements to nodes
	nodes := make([]models.GraphNode, len(elements))
	for i, element := range elements {
		nodes[i] = models.GraphNode{
			ID:     element.ID,
			Labels: []string{string(element.Type)},
			Properties: map[string]interface{}{
				"name":      element.Name,
				"type":      string(element.Type),
				"package":   element.Package,
				"file_path": element.FilePath,
			},
		}
	}

	relationships, err := s.postgresStorage.GetRelationships(ctx, options.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to get relationships: %w", err)
	}

	edges := make([]models.GraphEdge, len(relationships))
	for i, rel := range relationships {
		edges[i] = models.GraphEdge{
			ID:         rel.ID,
			From:       rel.FromID,
			To:         rel.ToID,
			Type:       string(rel.Type),
			Properties: rel.Properties,
		}
	}

	return &KnowledgeGraphData{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

func (s *Service) generateConnectionsFromPostgres(ctx context.Context, elementID string, _ int) (*KnowledgeGraphData, error) {
	relationships, err := s.postgresStorage.GetRelationships(ctx, elementID)
	if err != nil {
		return nil, fmt.Errorf("failed to get relationships: %w", err)
	}

	connectedIDs := make(map[string]bool)
	connectedIDs[elementID] = true

	for _, rel := range relationships {
		connectedIDs[rel.FromID] = true
		connectedIDs[rel.ToID] = true
	}

	var nodes []models.GraphNode
	for id := range connectedIDs {
		element, err := s.postgresStorage.GetCodeElement(ctx, id)
		if err != nil || element == nil {
			continue
		}

		nodes = append(nodes, models.GraphNode{
			ID:     element.ID,
			Labels: []string{string(element.Type)},
			Properties: map[string]interface{}{
				"name":      element.Name,
				"type":      string(element.Type),
				"package":   element.Package,
				"file_path": element.FilePath,
			},
		})
	}

	edges := make([]models.GraphEdge, len(relationships))
	for i, rel := range relationships {
		edges[i] = models.GraphEdge{
			ID:         rel.ID,
			From:       rel.FromID,
			To:         rel.ToID,
			Type:       string(rel.Type),
			Properties: rel.Properties,
		}
	}

	return &KnowledgeGraphData{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

func (s *Service) limitGraph(nodes []models.GraphNode, edges []models.GraphEdge, maxNodes int) ([]models.GraphNode, []models.GraphEdge) {
	if len(nodes) <= maxNodes {
		return nodes, edges
	}

	// take first maxNodes nodes and related edges
	limitedNodes := nodes[:maxNodes]
	nodeIDs := make(map[string]bool)
	for _, node := range limitedNodes {
		nodeIDs[node.ID] = true
	}

	var limitedEdges []models.GraphEdge
	for _, edge := range edges {
		if nodeIDs[edge.From] && nodeIDs[edge.To] {
			limitedEdges = append(limitedEdges, edge)
		}
	}

	return limitedNodes, limitedEdges
}

func (s *Service) enrichNodesWithCode(ctx context.Context, nodes []models.GraphNode) error {
	for i, node := range nodes {
		element, err := s.postgresStorage.GetCodeElement(ctx, node.ID)
		if err != nil || element == nil {
			continue
		}

		nodes[i].Properties["code_snippet"] = s.getCodeSnippet(element.Code, 100)
		nodes[i].Properties["doc_comment"] = element.DocComment
		nodes[i].Properties["signature"] = element.Signature
	}

	return nil
}

func (s *Service) getCodeSnippet(code string, maxLength int) string {
	if len(code) <= maxLength {
		return code
	}
	return code[:maxLength] + "..."
}

func (s *Service) calculateConnectedComponents(graph *KnowledgeGraphData) int {
	visited := make(map[string]bool)
	adjacency := make(map[string][]string)
	for _, edge := range graph.Edges {
		adjacency[edge.From] = append(adjacency[edge.From], edge.To)
		adjacency[edge.To] = append(adjacency[edge.To], edge.From)
	}

	components := 0
	for _, node := range graph.Nodes {
		if !visited[node.ID] {
			s.dfs(node.ID, adjacency, visited)
			components++
		}
	}

	return components
}

func (s *Service) dfs(nodeID string, adjacency map[string][]string, visited map[string]bool) {
	visited[nodeID] = true
	for _, neighbor := range adjacency[nodeID] {
		if !visited[neighbor] {
			s.dfs(neighbor, adjacency, visited)
		}
	}
}

func (s *Service) calculateCentralityMetrics(graph *KnowledgeGraphData) map[string]interface{} {
	metrics := make(map[string]interface{})

	inDegree := make(map[string]int)
	outDegree := make(map[string]int)

	for _, edge := range graph.Edges {
		outDegree[edge.From]++
		inDegree[edge.To]++
	}

	maxInDegree := 0
	maxOutDegree := 0
	var mostIncomingNode, mostOutgoingNode string

	for _, node := range graph.Nodes {
		if inDegree[node.ID] > maxInDegree {
			maxInDegree = inDegree[node.ID]
			mostIncomingNode = node.ID
		}
		if outDegree[node.ID] > maxOutDegree {
			maxOutDegree = outDegree[node.ID]
			mostOutgoingNode = node.ID
		}
	}

	metrics["max_in_degree"] = maxInDegree
	metrics["max_out_degree"] = maxOutDegree
	metrics["most_incoming_node"] = mostIncomingNode
	metrics["most_outgoing_node"] = mostOutgoingNode

	return metrics
}

func (s *Service) calculateMaxDepth(graph *KnowledgeGraphData) int {
	// find longest path using DFS
	adjacency := make(map[string][]string)
	for _, edge := range graph.Edges {
		adjacency[edge.From] = append(adjacency[edge.From], edge.To)
	}

	maxDepth := 0
	visited := make(map[string]bool)

	for _, node := range graph.Nodes {
		if !visited[node.ID] {
			depth := s.dfsDepth(node.ID, adjacency, visited, 0)
			if depth > maxDepth {
				maxDepth = depth
			}
		}
	}

	return maxDepth
}

func (s *Service) dfsDepth(nodeID string, adjacency map[string][]string, visited map[string]bool, currentDepth int) int {
	visited[nodeID] = true
	maxChildDepth := currentDepth

	for _, neighbor := range adjacency[nodeID] {
		if !visited[neighbor] {
			depth := s.dfsDepth(neighbor, adjacency, visited, currentDepth+1)
			if depth > maxChildDepth {
				maxChildDepth = depth
			}
		}
	}

	return maxChildDepth
}

func (s *Service) findPathBFS(_ []models.GraphNode, edges []models.GraphEdge, fromID, toID string) []string {
	// Build adjacency list
	adjacency := make(map[string][]string)
	for _, edge := range edges {
		adjacency[edge.From] = append(adjacency[edge.From], edge.To)
		adjacency[edge.To] = append(adjacency[edge.To], edge.From)
	}

	// BFS to find shortest path
	queue := []string{fromID}
	visited := make(map[string]bool)
	parent := make(map[string]string)
	visited[fromID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current == toID {
			// Reconstruct path
			path := []string{}
			for node := toID; node != ""; node = parent[node] {
				path = append([]string{node}, path...)
				if node == fromID {
					break
				}
			}
			return path
		}

		for _, neighbor := range adjacency[current] {
			if !visited[neighbor] {
				visited[neighbor] = true
				parent[neighbor] = current
				queue = append(queue, neighbor)
			}
		}
	}

	return []string{} // No path found
}

func (s *Service) extractPathElements(allNodes []models.GraphNode, allEdges []models.GraphEdge, path []string) ([]models.GraphNode, []models.GraphEdge) {
	pathNodeMap := make(map[string]bool)
	for _, nodeID := range path {
		pathNodeMap[nodeID] = true
	}

	var pathNodes []models.GraphNode
	for _, node := range allNodes {
		if pathNodeMap[node.ID] {
			pathNodes = append(pathNodes, node)
		}
	}

	var pathEdges []models.GraphEdge
	for _, edge := range allEdges {
		if pathNodeMap[edge.From] && pathNodeMap[edge.To] {
			pathEdges = append(pathEdges, edge)
		}
	}

	return pathNodes, pathEdges
}
