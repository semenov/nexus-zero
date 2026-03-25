package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// Server holds shared dependencies used by HTTP handlers.
type Server struct {
	store *Store
	hub   *Hub
}

// NewServer creates a Server with the given store and hub.
func NewServer(store *Store, hub *Hub) *Server {
	return &Server{store: store, hub: hub}
}

// -------------------------------------------------------------------
// Helper utilities
// -------------------------------------------------------------------

// writeJSON serialises v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON encode error: %v", err)
	}
}

// writeError writes a standard {"error":"..."} JSON response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// -------------------------------------------------------------------
// POST /v1/users
// -------------------------------------------------------------------

type registerRequest struct {
	IdentityKey   string `json:"identity_key"`
	EncryptionKey string `json:"encryption_key"`
}

// HandleRegister handles user registration. No authentication is required.
func (s *Server) HandleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.IdentityKey == "" || req.EncryptionKey == "" {
		writeError(w, http.StatusBadRequest, "identity_key and encryption_key are required")
		return
	}

	if err := s.store.CreateUser(r.Context(), req.IdentityKey, req.EncryptionKey); err != nil {
		if err.Error() == "conflict" {
			writeError(w, http.StatusConflict, "user already exists")
			return
		}
		log.Printf("HandleRegister CreateUser error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	user, err := s.store.GetUser(r.Context(), req.IdentityKey)
	if err != nil || user == nil {
		log.Printf("HandleRegister GetUser error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, user)
}

// -------------------------------------------------------------------
// GET /v1/users/{identity_key}
// -------------------------------------------------------------------

// HandleGetUser returns the public profile of a user. No authentication needed.
func (s *Server) HandleGetUser(w http.ResponseWriter, r *http.Request) {
	identityKey := chi.URLParam(r, "identity_key")
	if identityKey == "" {
		writeError(w, http.StatusBadRequest, "identity_key path parameter is required")
		return
	}

	user, err := s.store.GetUser(r.Context(), identityKey)
	if err != nil {
		log.Printf("HandleGetUser GetUser error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if user == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	writeJSON(w, http.StatusOK, user)
}

// -------------------------------------------------------------------
// POST /v1/messages
// -------------------------------------------------------------------

type sendMessageRequest struct {
	RecipientKey       string `json:"recipient_key"`
	EphemeralKey       string `json:"ephemeral_key"`
	Ciphertext         string `json:"ciphertext"`
	SenderEphemeralKey string `json:"sender_ephemeral_key"`
	SenderCiphertext   string `json:"sender_ciphertext"`
}

type sendMessageResponse struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

// HandleSendMessage stores an encrypted message and attempts real-time delivery
// via the WebSocket hub.
func (s *Server) HandleSendMessage(w http.ResponseWriter, r *http.Request) {
	senderKey := identityFromCtx(r.Context())

	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.RecipientKey == "" || req.EphemeralKey == "" || req.Ciphertext == "" {
		writeError(w, http.StatusBadRequest, "recipient_key, ephemeral_key, and ciphertext are required")
		return
	}

	// Verify recipient exists.
	recipient, err := s.store.GetUser(r.Context(), req.RecipientKey)
	if err != nil {
		log.Printf("HandleSendMessage GetUser error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if recipient == nil {
		writeError(w, http.StatusNotFound, "recipient not found")
		return
	}

	var senderEphKey, senderCT *string
	if req.SenderEphemeralKey != "" && req.SenderCiphertext != "" {
		senderEphKey = &req.SenderEphemeralKey
		senderCT = &req.SenderCiphertext
	}
	msg, err := s.store.SaveMessage(r.Context(), senderKey, req.RecipientKey, req.EphemeralKey, req.Ciphertext, senderEphKey, senderCT)
	if err != nil {
		log.Printf("HandleSendMessage SaveMessage error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Deliver to recipient; also echo to sender's other connected devices.
	s.hub.Deliver(req.RecipientKey, msg)
	s.hub.DeliverToSender(senderKey, msg)

	writeJSON(w, http.StatusCreated, sendMessageResponse{
		ID:        msg.ID,
		CreatedAt: msg.CreatedAt,
	})
}

// -------------------------------------------------------------------
// GET /v1/messages/history
// -------------------------------------------------------------------

// HandleGetHistory returns paginated message history for the authenticated user.
// Query params:
//   - limit:     max messages to return (default 100, max 500)
//   - since:     RFC3339Nano timestamp — return messages with created_at > since
//   - before_id: UUID — return limit messages older than this message ID
func (s *Server) HandleGetHistory(w http.ResponseWriter, r *http.Request) {
	ownerKey := identityFromCtx(r.Context())
	q := r.URL.Query()

	// Parse limit.
	limit := 100
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 500 {
		limit = 500
	}

	// Parse since.
	var since *time.Time
	if raw := q.Get("since"); raw != "" {
		if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			since = &t
		}
	}

	// Parse before_id.
	var beforeID *string
	if raw := q.Get("before_id"); raw != "" {
		beforeID = &raw
	}

	msgs, err := s.store.GetHistory(r.Context(), ownerKey, limit, since, beforeID)
	if err != nil {
		log.Printf("HandleGetHistory GetHistory error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if msgs == nil {
		msgs = []*Message{}
	}
	writeJSON(w, http.StatusOK, msgs)
}

// -------------------------------------------------------------------
// GET /v1/contacts
// -------------------------------------------------------------------

func (s *Server) HandleGetContacts(w http.ResponseWriter, r *http.Request) {
	ownerKey := identityFromCtx(r.Context())
	contacts, err := s.store.GetContacts(r.Context(), ownerKey)
	if err != nil {
		log.Printf("HandleGetContacts error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if contacts == nil {
		contacts = []*Contact{}
	}
	writeJSON(w, http.StatusOK, contacts)
}

// -------------------------------------------------------------------
// PUT /v1/contacts/{contact_key}
// -------------------------------------------------------------------

type upsertContactRequest struct {
	Nickname string `json:"nickname"`
}

func (s *Server) HandleUpsertContact(w http.ResponseWriter, r *http.Request) {
	ownerKey := identityFromCtx(r.Context())
	contactKey := chi.URLParam(r, "contact_key")
	if contactKey == "" {
		writeError(w, http.StatusBadRequest, "contact_key path parameter is required")
		return
	}
	var req upsertContactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Nickname == "" {
		writeError(w, http.StatusBadRequest, "nickname is required")
		return
	}
	if err := s.store.UpsertContact(r.Context(), ownerKey, contactKey, req.Nickname); err != nil {
		log.Printf("HandleUpsertContact error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// -------------------------------------------------------------------
// DELETE /v1/contacts/{contact_key}
// -------------------------------------------------------------------

func (s *Server) HandleDeleteContact(w http.ResponseWriter, r *http.Request) {
	ownerKey := identityFromCtx(r.Context())
	contactKey := chi.URLParam(r, "contact_key")
	if contactKey == "" {
		writeError(w, http.StatusBadRequest, "contact_key path parameter is required")
		return
	}
	if err := s.store.DeleteContact(r.Context(), ownerKey, contactKey); err != nil {
		log.Printf("HandleDeleteContact error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// -------------------------------------------------------------------
// GET /v1/messages/pending
// -------------------------------------------------------------------

// HandleGetPending returns all undelivered messages for the authenticated user
// and immediately marks them as delivered.
func (s *Server) HandleGetPending(w http.ResponseWriter, r *http.Request) {
	recipientKey := identityFromCtx(r.Context())

	msgs, err := s.store.GetPendingMessages(r.Context(), recipientKey)
	if err != nil {
		log.Printf("HandleGetPending GetPendingMessages error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Mark all fetched messages as delivered.
	if len(msgs) > 0 {
		ids := make([]string, len(msgs))
		for i, m := range msgs {
			ids[i] = m.ID
		}
		if err := s.store.MarkDelivered(r.Context(), ids); err != nil {
			log.Printf("HandleGetPending MarkDelivered error: %v", err)
			// Non-fatal: return messages anyway.
		}
	}

	// Return an empty JSON array rather than null when there are no messages.
	if msgs == nil {
		msgs = []*Message{}
	}
	writeJSON(w, http.StatusOK, msgs)
}
