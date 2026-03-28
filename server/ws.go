package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ── Hub ──────────────────────────────────────────────────────────────────────

// Hub maintains a thread-safe registry of active WebSocket clients, keyed by
// identity key. Multiple clients may share the same identity key.
type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[*wsClient]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: make(map[string]map[*wsClient]struct{})}
}

func (h *Hub) register(identityKey string, c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[identityKey] == nil {
		h.clients[identityKey] = make(map[*wsClient]struct{})
	}
	h.clients[identityKey][c] = struct{}{}
}

func (h *Hub) unregister(identityKey string, c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.clients[identityKey]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.clients, identityKey)
		}
	}
}

// Deliver pushes a message envelope to all connected clients for the given
// identity key. Non-blocking; drops if the buffer is full.
func (h *Hub) Deliver(identityKey string, env wsMessageEnvelope) {
	h.mu.RLock()
	set, ok := h.clients[identityKey]
	if !ok {
		h.mu.RUnlock()
		return
	}
	targets := make([]*wsClient, 0, len(set))
	for c := range set {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	for _, c := range targets {
		select {
		case c.send <- env:
		default:
			log.Printf("ws: send buffer full for %s, dropping message %s", identityKey, env.ID)
		}
	}
}

// DeliverEvent pushes a membership event to all connected clients for the
// given identity key.
func (h *Hub) DeliverEvent(identityKey string, event wsMemberEvent) {
	h.mu.RLock()
	set, ok := h.clients[identityKey]
	if !ok {
		h.mu.RUnlock()
		return
	}
	targets := make([]*wsClient, 0, len(set))
	for c := range set {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	for _, c := range targets {
		select {
		case c.send <- event:
		default:
		}
	}
}

// ── WebSocket types ──────────────────────────────────────────────────────────

type wsClient struct {
	identityKey string
	conn        *websocket.Conn
	send        chan any // wsMessageEnvelope or wsMemberEvent
}

type wsMessageEnvelope struct {
	Type           string    `json:"type"`
	ID             string    `json:"id"`
	NexusID        string    `json:"nexus_id"`
	SenderKey      string    `json:"sender_key"`
	SenderUsername string    `json:"sender_username,omitempty"`
	EphemeralKey   string    `json:"ephemeral_key"`
	Ciphertext     string    `json:"ciphertext"`
	CreatedAt      time.Time `json:"created_at"`
}

type wsMemberEvent struct {
	Type        string `json:"type"` // member_joined, member_left, member_kicked
	NexusID     string `json:"nexus_id"`
	IdentityKey string `json:"identity_key"`
	Username    string `json:"username,omitempty"`
}

type wsAck struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// ── Upgrader ─────────────────────────────────────────────────────────────────

var upgrader = websocket.Upgrader{
	HandshakeTimeout: 10 * time.Second,
	ReadBufferSize:   1024,
	WriteBufferSize:  4096,
	CheckOrigin:      func(r *http.Request) bool { return true },
}

// ── ServeWS ──────────────────────────────────────────────────────────────────

func (s *Server) ServeWS(w http.ResponseWriter, r *http.Request) {
	authParam := r.URL.Query().Get("auth")
	if authParam == "" {
		writeError(w, http.StatusUnauthorized, "missing auth query parameter")
		return
	}

	identityKey, err := verifyAuthHeader("Ed25519 " + authParam)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws: upgrade error for %s: %v", identityKey, err)
		return
	}

	c := &wsClient{
		identityKey: identityKey,
		conn:        conn,
		send:        make(chan any, 64),
	}
	s.hub.register(identityKey, c)
	defer s.hub.unregister(identityKey, c)

	log.Printf("ws: client connected: %s", identityKey)

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.readLoop(s.store)
	}()

	c.writeLoop(done)
	log.Printf("ws: client disconnected: %s", identityKey)
}

func (c *wsClient) readLoop(store *Store) {
	defer c.conn.Close()
	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("ws: read error for %s: %v", c.identityKey, err)
			}
			return
		}

		var ack wsAck
		if err := json.Unmarshal(raw, &ack); err != nil {
			continue
		}
		if ack.Type != "ack" || ack.ID == "" {
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := store.MarkDelivered(ctx, []string{ack.ID}); err != nil {
			log.Printf("ws: MarkDelivered error for %s: %v", ack.ID, err)
		}
		cancel()
	}
}

func (c *wsClient) writeLoop(done <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteJSON(msg); err != nil {
				log.Printf("ws: write error for %s: %v", c.identityKey, err)
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}
