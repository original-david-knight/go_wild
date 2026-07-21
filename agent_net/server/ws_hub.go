package server

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Ping interval for WebSocket keepalive.
	wsPingInterval = 30 * time.Second

	// Pong wait timeout — if no pong received within this, connection is closed.
	wsPongWait = 60 * time.Second

	// Write deadline for sending messages to client.
	wsWriteWait = 10 * time.Second

	// Send channel buffer size per client.
	wsSendBufferSize = 64
)

// WSNotification is a server-to-client WebSocket message.
type WSNotification struct {
	Type          string `json:"type"`
	MessageID     string `json:"message_id,omitempty"`
	FromPublicKey string `json:"from_public_key,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
}

// WSClient represents a connected WebSocket client.
type WSClient struct {
	pubKey string
	conn   *websocket.Conn
	send   chan []byte
	hub    *WSHub
}

// WSHub manages WebSocket connections for messaging notifications.
// One connection per agent (latest connection wins, old one is closed).
type WSHub struct {
	mu      sync.RWMutex
	clients map[string]*WSClient // pubkey -> client
}

// NewWSHub creates a new WebSocket hub.
func NewWSHub() *WSHub {
	return &WSHub{
		clients: make(map[string]*WSClient),
	}
}

// Register adds a new client, closing any existing connection for the same pubkey.
func (h *WSHub) Register(pubKey string, conn *websocket.Conn) *WSClient {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Close existing connection if any (latest wins).
	// Close the connection — the old pumps will detect the close and exit.
	// Don't close the send channel here; the old WritePump owns it.
	if existing, ok := h.clients[pubKey]; ok {
		existing.conn.Close()
	}

	client := &WSClient{
		pubKey: pubKey,
		conn:   conn,
		send:   make(chan []byte, wsSendBufferSize),
		hub:    h,
	}
	h.clients[pubKey] = client

	return client
}

// unregister removes a client from the hub.
// Only removes if the registered client matches the given one (prevents
// a stale connection from removing its replacement).
func (h *WSHub) unregister(pubKey string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.clients, pubKey)
}

// unregisterClient removes a specific client from the hub, only if it's
// still the registered client for that pubkey (prevents stale cleanup).
func (h *WSHub) unregisterClient(c *WSClient) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if existing, ok := h.clients[c.pubKey]; ok && existing == c {
		delete(h.clients, c.pubKey)
	}
}

// NotifyNewMessage sends a new_message notification to a recipient if connected.
func (h *WSHub) NotifyNewMessage(recipientPubKey, messageID, fromPubKey string, createdAt time.Time) {
	h.mu.RLock()
	client, ok := h.clients[recipientPubKey]
	h.mu.RUnlock()

	if !ok {
		return // Recipient not connected
	}

	notification := WSNotification{
		Type:          "new_message",
		MessageID:     messageID,
		FromPublicKey: fromPubKey,
		CreatedAt:     createdAt.Format(time.RFC3339),
	}

	data, err := json.Marshal(notification)
	if err != nil {
		log.Printf("ws: failed to marshal notification: %v", err)
		return
	}

	// Non-blocking send
	select {
	case client.send <- data:
	default:
		// Channel full, skip notification
		log.Printf("ws: send buffer full for %s, dropping notification", recipientPubKey[:12])
	}
}

// NotifyMessageRead sends a message_read notification.
// The readerPubKey is the agent who read the message.
func (h *WSHub) NotifyMessageRead(readerPubKey, messageID string) {
	// Broadcast to all connected clients (the sender will see the read receipt)
	// In practice, we'd want to notify only the message sender, but since we
	// don't have the sender info here, we rely on the client to filter.
	h.mu.RLock()
	defer h.mu.RUnlock()

	notification := WSNotification{
		Type:          "message_read",
		MessageID:     messageID,
		FromPublicKey: readerPubKey,
	}

	data, err := json.Marshal(notification)
	if err != nil {
		return
	}

	// Send to all clients except the reader themselves
	for pubKey, client := range h.clients {
		if pubKey == readerPubKey {
			continue
		}
		select {
		case client.send <- data:
		default:
			// Buffer full, skip
		}
	}
}

// connectedCount returns the number of connected clients.
func (h *WSHub) connectedCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// isConnected checks if a specific agent is connected.
func (h *WSHub) isConnected(pubKey string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.clients[pubKey]
	return ok
}

// WritePump pumps messages from the hub to the WebSocket connection.
// Should be run as a goroutine per client.
func (c *WSClient) WritePump() {
	ticker := time.NewTicker(wsPingInterval)
	defer func() {
		ticker.Stop()
		c.conn.Close()
		c.hub.unregisterClient(c)
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if !ok {
				// Hub closed the channel
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ReadPump reads messages from the WebSocket connection.
// We don't expect client-to-server messages, but we need to read
// to process control frames (pong) and detect disconnects.
func (c *WSClient) ReadPump() {
	defer func() {
		c.hub.unregisterClient(c)
		c.conn.Close()
	}()

	c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("ws: unexpected close for %s: %v", c.pubKey[:12], err)
			}
			break
		}
		// Discard any client-to-server messages (mutations via REST only)
	}
}
