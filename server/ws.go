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

// -------------------------------------------------------------------
// Hub — manages connected WebSocket clients
// -------------------------------------------------------------------

// Hub maintains a thread-safe registry of active WebSocket clients, keyed by
// identity key. Multiple clients (e.g. iOS + web) may share the same identity key.
type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[*wsClient]struct{}
}

// NewHub allocates a Hub.
func NewHub() *Hub {
	return &Hub{clients: make(map[string]map[*wsClient]struct{})}
}

// register adds a client to the hub. Multiple clients with the same identity
// key are all kept active (e.g. the same account open on iOS and web).
func (h *Hub) register(identityKey string, c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[identityKey] == nil {
		h.clients[identityKey] = make(map[*wsClient]struct{})
	}
	h.clients[identityKey][c] = struct{}{}
}

// unregister removes a specific client from the hub.
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

// IsConnected returns true if the identity key has at least one active WebSocket.
func (h *Hub) IsConnected(identityKey string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients[identityKey]) > 0
}

// Deliver pushes a message to all connected clients for the given identity key.
// Each send is non-blocking; a full buffer drops the message for that client
// (it can recover via GET /v1/messages/pending).
func (h *Hub) Deliver(identityKey string, msg *Message) {
	h.mu.RLock()
	set, ok := h.clients[identityKey]
	if !ok {
		h.mu.RUnlock()
		return
	}
	// Snapshot client pointers so we don't hold the lock while sending.
	targets := make([]*wsClient, 0, len(set))
	for c := range set {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	env := wsMessageEnvelope{
		Type:         "message",
		ID:           msg.ID,
		SenderKey:    msg.SenderKey,
		EphemeralKey: msg.EphemeralKey,
		Ciphertext:   msg.Ciphertext,
		CreatedAt:    msg.CreatedAt,
	}
	for _, c := range targets {
		select {
		case c.send <- env:
		default:
			log.Printf("ws: send buffer full for %s, dropping message %s", identityKey, msg.ID)
		}
	}
}

// DeliverToSender pushes a sent-message echo to all of the sender's connected
// clients other than the one that originated the send (identified by
// originConn, which may be nil to deliver to all). This allows other devices
// signed in with the same account to see outgoing messages in real time.
func (h *Hub) DeliverToSender(senderKey string, msg *Message) {
	if msg.SenderEphemeralKey == nil || msg.SenderCiphertext == nil {
		return // no sender copy, nothing to echo
	}
	h.mu.RLock()
	set, ok := h.clients[senderKey]
	if !ok {
		h.mu.RUnlock()
		return
	}
	targets := make([]*wsClient, 0, len(set))
	for c := range set {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	env := wsMessageEnvelope{
		Type:               "message",
		ID:                 msg.ID,
		SenderKey:          msg.SenderKey,
		RecipientKey:       msg.RecipientKey,
		EphemeralKey:       msg.EphemeralKey,
		Ciphertext:         msg.Ciphertext,
		SenderEphemeralKey: *msg.SenderEphemeralKey,
		SenderCiphertext:   *msg.SenderCiphertext,
		CreatedAt:          msg.CreatedAt,
	}
	for _, c := range targets {
		select {
		case c.send <- env:
		default:
			log.Printf("ws: send buffer full for sender echo %s, dropping message %s", senderKey, msg.ID)
		}
	}
}

// -------------------------------------------------------------------
// wsClient
// -------------------------------------------------------------------

// wsClient represents a single WebSocket connection.
type wsClient struct {
	identityKey string
	conn        *websocket.Conn
	send        chan wsMessageEnvelope
}

// wsMessageEnvelope is the outgoing JSON frame sent to clients.
// The optional fields are populated only when delivering an echo to the
// sender's other connected devices (so they can decrypt the sent message
// using the sender copy and know which conversation it belongs to).
type wsMessageEnvelope struct {
	Type               string    `json:"type"`
	ID                 string    `json:"id"`
	SenderKey          string    `json:"sender_key"`
	EphemeralKey       string    `json:"ephemeral_key"`
	Ciphertext         string    `json:"ciphertext"`
	CreatedAt          time.Time `json:"created_at"`
	RecipientKey       string    `json:"recipient_key,omitempty"`
	SenderEphemeralKey string    `json:"sender_ephemeral_key,omitempty"`
	SenderCiphertext   string    `json:"sender_ciphertext,omitempty"`
}

// wsAck is the incoming JSON frame from clients acknowledging delivery.
type wsAck struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// -------------------------------------------------------------------
// WebSocket upgrader
// -------------------------------------------------------------------

var upgrader = websocket.Upgrader{
	HandshakeTimeout: 10 * time.Second,
	ReadBufferSize:   1024,
	WriteBufferSize:  4096,
	// Allow all origins for WebSocket connections.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// -------------------------------------------------------------------
// ServeWS — HTTP handler that upgrades to WebSocket
// -------------------------------------------------------------------

// ServeWS upgrades the connection to WebSocket, authenticates via the ?auth=
// query parameter (same format as the Authorization header value without the
// "Ed25519 " scheme prefix), and then pumps outgoing messages and incoming
// ACKs.
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
		send:        make(chan wsMessageEnvelope, 64),
	}
	s.hub.register(identityKey, c)
	defer s.hub.unregister(identityKey, c)

	log.Printf("ws: client connected: %s", identityKey)

	// done is closed by the read loop when it exits.
	done := make(chan struct{})

	go func() {
		defer close(done)
		c.readLoop(s.store)
	}()

	c.writeLoop(done)
	log.Printf("ws: client disconnected: %s", identityKey)
}

// readLoop reads incoming frames (ACKs) from the client. Any other frame type
// is silently ignored.
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

// writeLoop writes outgoing messages from the send channel to the WebSocket.
// It also sends periodic pings to detect dead connections.
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
