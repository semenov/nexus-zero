package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// Server holds shared dependencies used by HTTP handlers.
type Server struct {
	store  *Store
	hub    *Hub
	pusher *Pusher
}

// NewServer creates a Server with the given store, hub, and pusher.
func NewServer(store *Store, hub *Hub, pusher *Pusher) *Server {
	return &Server{store: store, hub: hub, pusher: pusher}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON encode error: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ── POST /v1/users ───────────────────────────────────────────────────────────

type registerRequest struct {
	IdentityKey   string  `json:"identity_key"`
	EncryptionKey string  `json:"encryption_key"`
	Username      *string `json:"username,omitempty"`
}

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

	if err := s.store.CreateUser(r.Context(), req.IdentityKey, req.EncryptionKey, req.Username); err != nil {
		if err.Error() == "conflict" {
			writeError(w, http.StatusConflict, "user already exists")
			return
		}
		log.Printf("HandleRegister error: %v", err)
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

// ── GET /v1/users/{identity_key} ─────────────────────────────────────────────

func (s *Server) HandleGetUser(w http.ResponseWriter, r *http.Request) {
	identityKey := chi.URLParam(r, "identity_key")
	if identityKey == "" {
		writeError(w, http.StatusBadRequest, "identity_key is required")
		return
	}
	user, err := s.store.GetUser(r.Context(), identityKey)
	if err != nil {
		log.Printf("HandleGetUser error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if user == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// ── PUT /v1/users/me/username ────────────────────────────────────────────────

type setUsernameRequest struct {
	Username string `json:"username"`
}

func (s *Server) HandleSetUsername(w http.ResponseWriter, r *http.Request) {
	identityKey := identityFromCtx(r.Context())
	var req setUsernameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}
	if len(req.Username) < 2 || len(req.Username) > 32 {
		writeError(w, http.StatusBadRequest, "username must be 2-32 characters")
		return
	}
	if err := s.store.SetUsername(r.Context(), identityKey, req.Username); err != nil {
		switch err.Error() {
		case "username_taken":
			writeError(w, http.StatusConflict, "username already taken")
		case "not_found":
			writeError(w, http.StatusNotFound, "user not found")
		default:
			log.Printf("HandleSetUsername error: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── POST /v1/nexuses ─────────────────────────────────────────────────────────

type createNexusRequest struct {
	Name string `json:"name"`
}

func (s *Server) HandleCreateNexus(w http.ResponseWriter, r *http.Request) {
	creatorKey := identityFromCtx(r.Context())
	var req createNexusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	nexus, err := s.store.CreateNexus(r.Context(), req.Name, creatorKey)
	if err != nil {
		log.Printf("HandleCreateNexus error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusCreated, nexus)
}

// ── GET /v1/nexuses ──────────────────────────────────────────────────────────

func (s *Server) HandleGetNexuses(w http.ResponseWriter, r *http.Request) {
	identityKey := identityFromCtx(r.Context())
	nexuses, err := s.store.GetUserNexuses(r.Context(), identityKey)
	if err != nil {
		log.Printf("HandleGetNexuses error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if nexuses == nil {
		nexuses = []*Nexus{}
	}
	writeJSON(w, http.StatusOK, nexuses)
}

// ── GET /v1/nexuses/{id} ─────────────────────────────────────────────────────

type nexusDetailResponse struct {
	Nexus
	Members []*NexusMember `json:"members"`
}

func (s *Server) HandleGetNexus(w http.ResponseWriter, r *http.Request) {
	identityKey := identityFromCtx(r.Context())
	nexusID := chi.URLParam(r, "id")

	// Must be a member to view.
	member, err := s.store.IsNexusMember(r.Context(), nexusID, identityKey)
	if err != nil {
		log.Printf("HandleGetNexus IsNexusMember error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !member {
		writeError(w, http.StatusForbidden, "not a member of this nexus")
		return
	}

	nexus, err := s.store.GetNexus(r.Context(), nexusID)
	if err != nil {
		log.Printf("HandleGetNexus error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if nexus == nil {
		writeError(w, http.StatusNotFound, "nexus not found")
		return
	}

	members, err := s.store.GetNexusMembers(r.Context(), nexusID)
	if err != nil {
		log.Printf("HandleGetNexus GetMembers error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if members == nil {
		members = []*NexusMember{}
	}

	writeJSON(w, http.StatusOK, nexusDetailResponse{Nexus: *nexus, Members: members})
}

// ── PUT /v1/nexuses/{id} ─────────────────────────────────────────────────────

type updateNexusRequest struct {
	Name string `json:"name"`
}

func (s *Server) HandleUpdateNexus(w http.ResponseWriter, r *http.Request) {
	identityKey := identityFromCtx(r.Context())
	nexusID := chi.URLParam(r, "id")

	admin, err := s.store.IsNexusAdmin(r.Context(), nexusID, identityKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !admin {
		writeError(w, http.StatusForbidden, "admin access required")
		return
	}

	var req updateNexusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := s.store.UpdateNexus(r.Context(), nexusID, req.Name); err != nil {
		log.Printf("HandleUpdateNexus error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── DELETE /v1/nexuses/{id} ──────────────────────────────────────────────────

func (s *Server) HandleDeleteNexus(w http.ResponseWriter, r *http.Request) {
	identityKey := identityFromCtx(r.Context())
	nexusID := chi.URLParam(r, "id")

	admin, err := s.store.IsNexusAdmin(r.Context(), nexusID, identityKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !admin {
		writeError(w, http.StatusForbidden, "admin access required")
		return
	}
	if err := s.store.DeleteNexus(r.Context(), nexusID); err != nil {
		log.Printf("HandleDeleteNexus error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── GET /v1/nexuses/{id}/members ─────────────────────────────────────────────

func (s *Server) HandleGetMembers(w http.ResponseWriter, r *http.Request) {
	identityKey := identityFromCtx(r.Context())
	nexusID := chi.URLParam(r, "id")

	member, err := s.store.IsNexusMember(r.Context(), nexusID, identityKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !member {
		writeError(w, http.StatusForbidden, "not a member")
		return
	}

	members, err := s.store.GetNexusMembers(r.Context(), nexusID)
	if err != nil {
		log.Printf("HandleGetMembers error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if members == nil {
		members = []*NexusMember{}
	}
	writeJSON(w, http.StatusOK, members)
}

// ── DELETE /v1/nexuses/{id}/members/{identity_key} ───────────────────────────

func (s *Server) HandleKickMember(w http.ResponseWriter, r *http.Request) {
	adminKey := identityFromCtx(r.Context())
	nexusID := chi.URLParam(r, "id")
	targetKey := chi.URLParam(r, "identity_key")

	admin, err := s.store.IsNexusAdmin(r.Context(), nexusID, adminKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !admin {
		writeError(w, http.StatusForbidden, "admin access required")
		return
	}
	if targetKey == adminKey {
		writeError(w, http.StatusBadRequest, "cannot kick yourself")
		return
	}

	if err := s.store.KickMember(r.Context(), nexusID, targetKey); err != nil {
		log.Printf("HandleKickMember error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Notify online members about the kick.
	s.broadcastMemberEvent(r.Context(), nexusID, "member_kicked", targetKey)

	w.WriteHeader(http.StatusNoContent)
}

// ── POST /v1/nexuses/{id}/leave ──────────────────────────────────────────────

func (s *Server) HandleLeaveNexus(w http.ResponseWriter, r *http.Request) {
	identityKey := identityFromCtx(r.Context())
	nexusID := chi.URLParam(r, "id")

	if err := s.store.RemoveNexusMember(r.Context(), nexusID, identityKey); err != nil {
		log.Printf("HandleLeaveNexus error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	s.broadcastMemberEvent(r.Context(), nexusID, "member_left", identityKey)

	w.WriteHeader(http.StatusNoContent)
}

// ── POST /v1/nexuses/{id}/invites ────────────────────────────────────────────

type createInviteRequest struct {
	MaxUses        *int `json:"max_uses,omitempty"`
	ExpiresInHours *int `json:"expires_in_hours,omitempty"`
}

func (s *Server) HandleCreateInvite(w http.ResponseWriter, r *http.Request) {
	identityKey := identityFromCtx(r.Context())
	nexusID := chi.URLParam(r, "id")

	admin, err := s.store.IsNexusAdmin(r.Context(), nexusID, identityKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !admin {
		writeError(w, http.StatusForbidden, "admin access required")
		return
	}

	var req createInviteRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // all fields optional

	var expiresAt *time.Time
	if req.ExpiresInHours != nil && *req.ExpiresInHours > 0 {
		t := time.Now().Add(time.Duration(*req.ExpiresInHours) * time.Hour)
		expiresAt = &t
	}

	code, err := s.store.CreateInviteCode(r.Context(), nexusID, identityKey, req.MaxUses, expiresAt)
	if err != nil {
		log.Printf("HandleCreateInvite error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusCreated, code)
}

// ── GET /v1/nexuses/{id}/invites ─────────────────────────────────────────────

func (s *Server) HandleGetInvites(w http.ResponseWriter, r *http.Request) {
	identityKey := identityFromCtx(r.Context())
	nexusID := chi.URLParam(r, "id")

	admin, err := s.store.IsNexusAdmin(r.Context(), nexusID, identityKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !admin {
		writeError(w, http.StatusForbidden, "admin access required")
		return
	}

	codes, err := s.store.GetInviteCodes(r.Context(), nexusID)
	if err != nil {
		log.Printf("HandleGetInvites error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if codes == nil {
		codes = []*InviteCode{}
	}
	writeJSON(w, http.StatusOK, codes)
}

// ── DELETE /v1/nexuses/{id}/invites/{invite_id} ──────────────────────────────

func (s *Server) HandleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	identityKey := identityFromCtx(r.Context())
	nexusID := chi.URLParam(r, "id")

	admin, err := s.store.IsNexusAdmin(r.Context(), nexusID, identityKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !admin {
		writeError(w, http.StatusForbidden, "admin access required")
		return
	}

	inviteID := chi.URLParam(r, "invite_id")
	if err := s.store.RevokeInviteCode(r.Context(), inviteID); err != nil {
		log.Printf("HandleRevokeInvite error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── POST /v1/join ────────────────────────────────────────────────────────────

type joinRequest struct {
	Code string `json:"code"`
}

type joinResponse struct {
	NexusID string `json:"nexus_id"`
	Name    string `json:"name"`
}

func (s *Server) HandleJoinNexus(w http.ResponseWriter, r *http.Request) {
	identityKey := identityFromCtx(r.Context())
	var req joinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}

	nexusID, err := s.store.ValidateAndUseInviteCode(r.Context(), req.Code, identityKey)
	if err != nil {
		switch err.Error() {
		case "invalid_code":
			writeError(w, http.StatusNotFound, "invalid invite code")
		case "code_revoked":
			writeError(w, http.StatusGone, "invite code has been revoked")
		case "code_expired":
			writeError(w, http.StatusGone, "invite code has expired")
		case "code_exhausted":
			writeError(w, http.StatusGone, "invite code has reached its usage limit")
		case "kicked":
			writeError(w, http.StatusForbidden, "you have been kicked from this nexus")
		case "already_member":
			writeError(w, http.StatusConflict, "you are already a member")
		default:
			log.Printf("HandleJoinNexus error: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	nexus, err := s.store.GetNexus(r.Context(), nexusID)
	if err != nil || nexus == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Notify existing members.
	s.broadcastMemberEvent(r.Context(), nexusID, "member_joined", identityKey)

	writeJSON(w, http.StatusOK, joinResponse{NexusID: nexus.ID, Name: nexus.Name})
}

// ── POST /v1/nexuses/{id}/messages ───────────────────────────────────────────

type sendNexusMessageRequest struct {
	Envelopes []MessageEnvelope `json:"envelopes"`
}

type sendNexusMessageResponse struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Server) HandleSendNexusMessage(w http.ResponseWriter, r *http.Request) {
	senderKey := identityFromCtx(r.Context())
	nexusID := chi.URLParam(r, "id")

	// Verify sender is a member.
	member, err := s.store.IsNexusMember(r.Context(), nexusID, senderKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !member {
		writeError(w, http.StatusForbidden, "not a member of this nexus")
		return
	}

	var req sendNexusMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.Envelopes) == 0 {
		writeError(w, http.StatusBadRequest, "envelopes are required")
		return
	}

	msgID, createdAt, err := s.store.SaveNexusMessages(r.Context(), nexusID, senderKey, req.Envelopes)
	if err != nil {
		log.Printf("HandleSendNexusMessage SaveNexusMessages error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Look up sender's username for the WS envelope.
	sender, _ := s.store.GetUser(r.Context(), senderKey)
	var senderUsername string
	if sender != nil && sender.Username != nil {
		senderUsername = *sender.Username
	}

	// Deliver to each recipient via WebSocket + push.
	for _, env := range req.Envelopes {
		wsEnv := wsMessageEnvelope{
			Type:           "message",
			ID:             msgID,
			NexusID:        nexusID,
			SenderKey:      senderKey,
			SenderUsername: senderUsername,
			EphemeralKey:   env.EphemeralKey,
			Ciphertext:     env.Ciphertext,
			CreatedAt:      createdAt,
		}
		s.hub.Deliver(env.RecipientKey, wsEnv)

		// Push notification (skip sender — they'll get the WS echo).
		if env.RecipientKey != senderKey {
			if tokens, err := s.store.GetDeviceTokens(r.Context(), env.RecipientKey); err == nil {
				s.pusher.SendToTokens(tokens)
			}
		}
	}

	writeJSON(w, http.StatusCreated, sendNexusMessageResponse{ID: msgID, CreatedAt: createdAt})
}

// ── GET /v1/nexuses/{id}/messages ────────────────────────────────────────────

func (s *Server) HandleGetNexusHistory(w http.ResponseWriter, r *http.Request) {
	identityKey := identityFromCtx(r.Context())
	nexusID := chi.URLParam(r, "id")

	member, err := s.store.IsNexusMember(r.Context(), nexusID, identityKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !member {
		writeError(w, http.StatusForbidden, "not a member")
		return
	}

	q := r.URL.Query()
	limit := 100
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 500 {
		limit = 500
	}

	var since *time.Time
	if raw := q.Get("since"); raw != "" {
		if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			since = &t
		}
	}

	var beforeID *string
	if raw := q.Get("before_id"); raw != "" {
		beforeID = &raw
	}

	msgs, err := s.store.GetNexusHistory(r.Context(), nexusID, identityKey, limit, since, beforeID)
	if err != nil {
		log.Printf("HandleGetNexusHistory error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if msgs == nil {
		msgs = []*Message{}
	}
	writeJSON(w, http.StatusOK, msgs)
}

// ── GET /v1/messages/pending ─────────────────────────────────────────────────

func (s *Server) HandleGetPending(w http.ResponseWriter, r *http.Request) {
	recipientKey := identityFromCtx(r.Context())

	msgs, err := s.store.GetPendingMessages(r.Context(), recipientKey)
	if err != nil {
		log.Printf("HandleGetPending error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if len(msgs) > 0 {
		ids := make([]string, len(msgs))
		for i, m := range msgs {
			ids[i] = m.ID
		}
		if err := s.store.MarkDelivered(r.Context(), ids); err != nil {
			log.Printf("HandleGetPending MarkDelivered error: %v", err)
		}
	}

	if msgs == nil {
		msgs = []*Message{}
	}
	writeJSON(w, http.StatusOK, msgs)
}

// ── PUT /v1/device-token ─────────────────────────────────────────────────────

type deviceTokenRequest struct {
	Token string `json:"token"`
}

func (s *Server) HandleUpsertDeviceToken(w http.ResponseWriter, r *http.Request) {
	identityKey := identityFromCtx(r.Context())
	var req deviceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	if err := s.store.UpsertDeviceToken(r.Context(), identityKey, req.Token); err != nil {
		log.Printf("HandleUpsertDeviceToken error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Member event broadcasting ────────────────────────────────────────────────

func (s *Server) broadcastMemberEvent(ctx context.Context, nexusID, eventType, targetKey string) {
	// Look up username for the target.
	var username string
	if u, err := s.store.GetUser(ctx, targetKey); err == nil && u != nil && u.Username != nil {
		username = *u.Username
	}

	members, err := s.store.GetNexusMembers(ctx, nexusID)
	if err != nil {
		return
	}
	event := wsMemberEvent{
		Type:        eventType,
		NexusID:     nexusID,
		IdentityKey: targetKey,
		Username:    username,
	}
	for _, m := range members {
		s.hub.DeliverEvent(m.IdentityKey, event)
	}
}
