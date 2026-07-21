package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWSHubRegisterUnregister(t *testing.T) {
	hub := NewWSHub()

	if hub.connectedCount() != 0 {
		t.Error("New hub should have 0 connections")
	}

	// Create a test WebSocket server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Upgrade failed: %v", err)
			return
		}

		client := hub.Register("agent1", conn)
		go client.WritePump()
	}))
	defer srv.Close()

	// Connect
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Give the server goroutine time to register
	time.Sleep(50 * time.Millisecond)

	if hub.connectedCount() != 1 {
		t.Errorf("Expected 1 connection, got %d", hub.connectedCount())
	}

	if !hub.isConnected("agent1") {
		t.Error("agent1 should be connected")
	}

	if hub.isConnected("agent2") {
		t.Error("agent2 should not be connected")
	}

	// unregister
	hub.unregister("agent1")

	if hub.connectedCount() != 0 {
		t.Errorf("Expected 0 connections after unregister, got %d", hub.connectedCount())
	}
}

func TestWSHubLatestWins(t *testing.T) {
	hub := NewWSHub()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		client := hub.Register("agent1", conn)
		go client.WritePump()
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	// First connection
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect 1: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Second connection (should replace first)
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect 2: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	defer conn1.Close()
	defer conn2.Close()

	// Should still have exactly 1 connection
	if hub.connectedCount() != 1 {
		t.Errorf("Expected 1 connection (latest wins), got %d", hub.connectedCount())
	}
}

func TestWSHubNotifyNewMessage(t *testing.T) {
	hub := NewWSHub()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		client := hub.Register("recipient_key", conn)
		go client.WritePump()
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	// Send notification
	now := time.Now()
	hub.NotifyNewMessage("recipient_key", "msg-123", "sender_key", now)

	// Read notification
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read notification: %v", err)
	}

	var notification WSNotification
	if err := json.Unmarshal(data, &notification); err != nil {
		t.Fatalf("Failed to decode notification: %v", err)
	}

	if notification.Type != "new_message" {
		t.Errorf("Expected type 'new_message', got '%s'", notification.Type)
	}
	if notification.MessageID != "msg-123" {
		t.Errorf("Expected message_id 'msg-123', got '%s'", notification.MessageID)
	}
	if notification.FromPublicKey != "sender_key" {
		t.Errorf("Expected from_public_key 'sender_key', got '%s'", notification.FromPublicKey)
	}
}

func TestWSHubNotifyDisconnectedAgent(t *testing.T) {
	hub := NewWSHub()

	// Notify a non-existent agent — should not panic
	hub.NotifyNewMessage("nobody", "msg-123", "sender", time.Now())
	hub.NotifyMessageRead("nobody", "msg-123")
}

func TestWSHubNotifyMessageRead(t *testing.T) {
	hub := NewWSHub()

	// Set up two clients
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		agentID := r.URL.Query().Get("agent_id")
		client := hub.Register(agentID, conn)
		go client.WritePump()
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	// Connect sender
	senderConn, _, err := websocket.DefaultDialer.Dial(wsURL+"?agent_id=sender_key", nil)
	if err != nil {
		t.Fatalf("Failed to connect sender: %v", err)
	}
	defer senderConn.Close()

	// Connect reader
	readerConn, _, err := websocket.DefaultDialer.Dial(wsURL+"?agent_id=reader_key", nil)
	if err != nil {
		t.Fatalf("Failed to connect reader: %v", err)
	}
	defer readerConn.Close()

	time.Sleep(50 * time.Millisecond)

	// Reader reads a message
	hub.NotifyMessageRead("reader_key", "msg-456")

	// Sender should receive the read notification
	senderConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := senderConn.ReadMessage()
	if err != nil {
		t.Fatalf("Sender failed to read notification: %v", err)
	}

	var notification WSNotification
	if err := json.Unmarshal(data, &notification); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if notification.Type != "message_read" {
		t.Errorf("Expected type 'message_read', got '%s'", notification.Type)
	}
	if notification.MessageID != "msg-456" {
		t.Errorf("Expected message_id 'msg-456', got '%s'", notification.MessageID)
	}
}
