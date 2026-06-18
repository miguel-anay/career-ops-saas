package ws

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// connEntry represents a single WebSocket client connection.
type connEntry struct {
	scanRunID uuid.UUID
	connID    string
	ch        chan []byte
}

type registerMsg struct {
	scanRunID uuid.UUID
	connID    string
	ch        chan []byte
}

type unregisterMsg struct {
	scanRunID uuid.UUID
	connID    string
}

type broadcastMsg struct {
	scanRunID uuid.UUID
	payload   []byte
}

// Hub manages WebSocket client connections keyed by scan_run_id.
// Connections are registered/unregistered via channels to avoid mutex contention.
type Hub struct {
	// connections: scan_run_id -> connID -> channel
	mu          sync.RWMutex
	connections map[uuid.UUID]map[string]chan []byte

	register   chan registerMsg
	unregister chan unregisterMsg
	broadcast  chan broadcastMsg
}

// NewHub creates a new Hub. Call Run() in a goroutine after creation.
func NewHub() *Hub {
	return &Hub{
		connections: make(map[uuid.UUID]map[string]chan []byte),
		register:    make(chan registerMsg, 64),
		unregister:  make(chan unregisterMsg, 64),
		broadcast:   make(chan broadcastMsg, 256),
	}
}

// Run starts the hub's event loop. Should be called as a goroutine.
// Exits when ctx is cancelled.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-h.register:
			h.mu.Lock()
			if _, ok := h.connections[msg.scanRunID]; !ok {
				h.connections[msg.scanRunID] = make(map[string]chan []byte)
			}
			h.connections[msg.scanRunID][msg.connID] = msg.ch
			h.mu.Unlock()

		case msg := <-h.unregister:
			h.mu.Lock()
			if conns, ok := h.connections[msg.scanRunID]; ok {
				if ch, ok := conns[msg.connID]; ok {
					close(ch)
					delete(conns, msg.connID)
				}
				if len(conns) == 0 {
					delete(h.connections, msg.scanRunID)
				}
			}
			h.mu.Unlock()

		case msg := <-h.broadcast:
			h.mu.RLock()
			conns := h.connections[msg.scanRunID]
			h.mu.RUnlock()

			for _, ch := range conns {
				select {
				case ch <- msg.payload:
				default:
					// Drop message if channel is full — client too slow.
				}
			}
		}
	}
}

// Register adds a connection to the hub for a given scan run.
func (h *Hub) Register(scanRunID uuid.UUID, connID string, ch chan []byte) {
	h.register <- registerMsg{scanRunID: scanRunID, connID: connID, ch: ch}
}

// Unregister removes a connection from the hub.
func (h *Hub) Unregister(scanRunID uuid.UUID, connID string) {
	h.unregister <- unregisterMsg{scanRunID: scanRunID, connID: connID}
}

// Broadcast sends a message to all connections subscribed to a scan run.
// The payload must be a valid JSON envelope:
//
//	{"event": "...", "run_id": "...", "ts": "2026-...", "data": {...}}
func (h *Hub) Broadcast(scanRunID uuid.UUID, msg []byte) {
	h.broadcast <- broadcastMsg{scanRunID: scanRunID, payload: msg}
}
