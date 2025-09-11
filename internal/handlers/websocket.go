package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/queue"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type WebSocketHandler struct {
	clients map[*Client]bool
	pubsub  *queue.RedisPubSub
	logger  *zap.Logger
	mu      sync.RWMutex
}

type Client struct {
	conn      net.Conn
	send      chan []byte
	projectID string
	handler   *WebSocketHandler
}

type Message struct {
	Type      string                 `json:"type"`
	ProjectID string                 `json:"project_id,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

func NewWebSocketHandler(pubsub *queue.RedisPubSub, logger *zap.Logger) *WebSocketHandler {
	return &WebSocketHandler{
		clients: make(map[*Client]bool),
		pubsub:  pubsub,
		logger:  logger,
	}
}

func (h *WebSocketHandler) Start() {
	ctx := context.Background()

	analysisChannel := h.pubsub.Subscribe(ctx, "analysis_updates")
	go h.handleRedisMessages(analysisChannel, "analysis_update")

	projectChannel := h.pubsub.Subscribe(ctx, "project_updates")
	go h.handleRedisMessages(projectChannel, "project_update")

	searchChannel := h.pubsub.Subscribe(ctx, "search_updates")
	go h.handleRedisMessages(searchChannel, "search_update")

	graphChannel := h.pubsub.Subscribe(ctx, "knowledge_graph_updates")
	go h.handleRedisMessages(graphChannel, "knowledge_graph_update")

	h.logger.Info("WebSocket handler started")
}

func (h *WebSocketHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		h.logger.Error("Failed to upgrade connection", zap.Error(err))
		return
	}

	client := &Client{
		conn:    conn,
		send:    make(chan []byte, 256),
		handler: h,
	}

	h.mu.Lock()
	h.clients[client] = true
	h.mu.Unlock()
	h.logger.Info("New WebSocket client connected")

	welcomeMsg := Message{
		Type:      "welcome",
		Data:      map[string]interface{}{"message": "Connected to Code Discovery Engine"},
		Timestamp: time.Now(),
	}
	client.sendMessage(welcomeMsg)

	go client.writePump()
	go client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		c.handler.unregisterClient(c)
		c.conn.Close()
	}()
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))

	for {
		messages, err := wsutil.ReadClientMessage(c.conn, nil)
		if err != nil {
			if err != io.EOF {
				c.handler.logger.Error("WebSocket read error", zap.Error(err))
			}
			break
		}

		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		for _, msg := range messages {
			if msg.OpCode == ws.OpText {
				var wsMsg Message
				if err := json.Unmarshal(msg.Payload, &wsMsg); err != nil {
					c.handler.logger.Error("Failed to unmarshal message", zap.Error(err))
					continue
				}
				c.handleMessage(wsMsg)
			} else if msg.OpCode == ws.OpPong {
				continue
			}
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				wsutil.WriteServerMessage(c.conn, ws.OpClose, nil)
				return
			}
			if err := wsutil.WriteServerMessage(c.conn, ws.OpText, message); err != nil {
				return
			}

			n := len(c.send)
		loop:
			for range n {
				select {
				case queuedMsg := <-c.send:
					if err := wsutil.WriteServerMessage(c.conn, ws.OpText, queuedMsg); err != nil {
						return
					}
				default:
					break loop
				}
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := wsutil.WriteServerMessage(c.conn, ws.OpPing, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) handleMessage(msg Message) {
	switch msg.Type {
	case "subscribe_project":
		if projectID, ok := msg.Data["project_id"].(string); ok {
			c.projectID = projectID
			c.handler.logger.Info("Client subscribed to project",
				zap.String("project_id", projectID))
			response := Message{
				Type:      "subscribed",
				ProjectID: projectID,
				Data:      map[string]interface{}{"status": "subscribed"},
				Timestamp: time.Now(),
			}
			c.sendMessage(response)
		}
	case "unsubscribe_project":
		c.projectID = ""
		response := Message{
			Type:      "unsubscribed",
			Data:      map[string]interface{}{"status": "unsubscribed"},
			Timestamp: time.Now(),
		}
		c.sendMessage(response)
	case "ping":
		response := Message{
			Type:      "pong",
			Timestamp: time.Now(),
		}
		c.sendMessage(response)
	default:
		c.handler.logger.Warn("Unknown message type", zap.String("type", msg.Type))
		response := Message{
			Type:      "error",
			Data:      map[string]interface{}{"message": "Unknown message type"},
			Timestamp: time.Now(),
		}
		c.sendMessage(response)
	}
}

func (c *Client) sendMessage(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		c.handler.logger.Error("Failed to marshal message", zap.Error(err))
		return
	}

	select {
	case c.send <- data:
	default:
		close(c.send)
	}
}

func (h *WebSocketHandler) unregisterClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.clients[client]; ok {
		delete(h.clients, client)
		close(client.send)
		h.logger.Info("WebSocket client disconnected")
	}
}

func (h *WebSocketHandler) handleRedisMessages(channel <-chan *redis.Message, messageType string) {
	for msg := range channel {
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(msg.Payload), &data); err != nil {
			h.logger.Error("Failed to unmarshal Redis message", zap.Error(err))
			continue
		}

		wsMessage := Message{
			Type:      messageType,
			Data:      data,
			Timestamp: time.Now(),
		}

		if projectID, ok := data["project_id"].(string); ok {
			wsMessage.ProjectID = projectID
		}

		h.broadcastMessage(wsMessage)
	}
}

func (h *WebSocketHandler) broadcastMessage(msg Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	messageData, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("Failed to marshal broadcast message", zap.Error(err))
		return
	}

	for client := range h.clients {
		if msg.ProjectID == "" || client.projectID == "" || client.projectID == msg.ProjectID {
			select {
			case client.send <- messageData:
			default:
				// Client's send channel is full, remove client
				delete(h.clients, client)
				close(client.send)
			}
		}
	}
}

func (h *WebSocketHandler) BroadcastToProject(projectID string, messageType string, data map[string]interface{}) {
	msg := Message{
		Type:      messageType,
		ProjectID: projectID,
		Data:      data,
		Timestamp: time.Now(),
	}
	h.broadcastMessage(msg)
}

func (h *WebSocketHandler) GetConnectedClients() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *WebSocketHandler) SendProgressUpdate(projectID, jobID string, progress int, status string, message string) {
	data := map[string]interface{}{
		"job_id":     jobID,
		"project_id": projectID,
		"progress":   progress,
		"status":     status,
		"message":    message,
	}
	h.BroadcastToProject(projectID, "analysis_progress", data)
}

func (h *WebSocketHandler) SendErrorNotification(projectID string, errorType, message string) {
	data := map[string]interface{}{
		"error_type": errorType,
		"message":    message,
	}
	h.BroadcastToProject(projectID, "error", data)
}
