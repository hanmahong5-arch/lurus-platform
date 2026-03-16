package sender

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/pkg/metrics"
)

// WSMessage is the JSON payload sent to WebSocket clients.
type WSMessage struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	ID      int64  `json:"id,omitempty"`
	Unread  int64  `json:"unread,omitempty"`
}

// Hub manages WebSocket connections grouped by account ID.
type Hub struct {
	mu    sync.RWMutex
	conns map[int64]map[*websocket.Conn]struct{}
}

// NewHub creates a new WebSocket connection hub.
func NewHub() *Hub {
	return &Hub{
		conns: make(map[int64]map[*websocket.Conn]struct{}),
	}
}

// Register adds a WebSocket connection for an account.
func (h *Hub) Register(accountID int64, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[accountID] == nil {
		h.conns[accountID] = make(map[*websocket.Conn]struct{})
	}
	h.conns[accountID][conn] = struct{}{}
	metrics.WebSocketConnections.Inc()
}

// Unregister removes a WebSocket connection.
func (h *Hub) Unregister(accountID int64, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m, ok := h.conns[accountID]; ok {
		delete(m, conn)
		if len(m) == 0 {
			delete(h.conns, accountID)
		}
		metrics.WebSocketConnections.Dec()
	}
}

// Broadcast sends a message to all connections for a given account.
func (h *Hub) Broadcast(accountID int64, msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Error("ws marshal error", "err", err)
		return
	}

	h.mu.RLock()
	conns := h.conns[accountID]
	h.mu.RUnlock()

	for conn := range conns {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			slog.Debug("ws write error, removing connection", "account_id", accountID, "err", err)
			h.Unregister(accountID, conn)
			conn.Close()
		}
	}
}

// WSSender delivers notifications via WebSocket push to connected clients.
type WSSender struct {
	hub *Hub
}

// NewWSSender creates a WebSocket sender backed by a connection hub.
func NewWSSender(hub *Hub) *WSSender {
	return &WSSender{hub: hub}
}

// Send pushes a notification to all WebSocket connections for the account.
// msg.To is expected to be the string representation of account_id.
func (s *WSSender) Send(_ context.Context, msg Message) error {
	// Account ID is passed via metadata since msg.To is a string
	accountIDStr, ok := msg.Metadata["account_id"]
	if !ok || accountIDStr == "" {
		return nil // no account to push to
	}

	// Parse account_id from string
	var accountID int64
	for _, c := range accountIDStr {
		if c < '0' || c > '9' {
			return nil
		}
		accountID = accountID*10 + int64(c-'0')
	}

	s.hub.Broadcast(accountID, WSMessage{
		Type:  "notification",
		Title: msg.Subject,
		Body:  msg.Body,
	})
	return nil
}

// Name returns the channel name.
func (s *WSSender) Name() string { return "websocket" }
