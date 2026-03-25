package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// ServerUser is the response from GET /v1/users/:key.
type ServerUser struct {
	IdentityKey   string    `json:"identity_key"`
	EncryptionKey string    `json:"encryption_key"`
	CreatedAt     time.Time `json:"created_at"`
}

// ServerMessage is the response from GET /v1/messages/pending and WebSocket frames.
type ServerMessage struct {
	ID           string    `json:"id"`
	SenderKey    string    `json:"sender_key"`
	RecipientKey string    `json:"recipient_key"`
	EphemeralKey string    `json:"ephemeral_key"`
	Ciphertext   string    `json:"ciphertext"`
	CreatedAt    time.Time `json:"created_at"`
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

// registerUser registers an identity+encryption keypair with the server.
func registerUser(serverURL, identKey, encKey, header string) error {
	body, _ := json.Marshal(map[string]string{
		"identity_key":   identKey,
		"encryption_key": encKey,
	})
	req, err := http.NewRequest("POST", serverURL+"/v1/users", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", header)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return nil // already registered, idempotent
	}
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, readBody(resp.Body))
	}
	return nil
}

// getUser fetches a user's public keys by identity key.
func getUser(serverURL, identKey string) (*ServerUser, error) {
	resp, err := httpClient.Get(serverURL + "/v1/users/" + url.PathEscape(identKey))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, readBody(resp.Body))
	}
	var u ServerUser
	return &u, json.NewDecoder(resp.Body).Decode(&u)
}

// sendMessage sends an encrypted message to a recipient, including a sender copy.
func sendMessage(serverURL, recipientKey, ephKey, ciphertext, senderEphKey, senderCT, header string) error {
	body, _ := json.Marshal(map[string]string{
		"recipient_key":        recipientKey,
		"ephemeral_key":        ephKey,
		"ciphertext":           ciphertext,
		"sender_ephemeral_key": senderEphKey,
		"sender_ciphertext":    senderCT,
	})
	req, err := http.NewRequest("POST", serverURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", header)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, readBody(resp.Body))
	}
	return nil
}

// upsertServerContact creates or updates a contact on the server.
func upsertServerContact(serverURL, contactKey, nickname, header string) error {
	body, _ := json.Marshal(map[string]string{"nickname": nickname})
	req, err := http.NewRequest("PUT", serverURL+"/v1/contacts/"+url.PathEscape(contactKey), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", header)
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, readBody(resp.Body))
	}
	return nil
}

// getPendingMessages fetches and marks-delivered all pending messages.
func getPendingMessages(serverURL, header string) ([]ServerMessage, error) {
	req, err := http.NewRequest("GET", serverURL+"/v1/messages/pending", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", header)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, readBody(resp.Body))
	}
	var msgs []ServerMessage
	return msgs, json.NewDecoder(resp.Body).Decode(&msgs)
}

// wsFrame is the JSON structure received from the WebSocket.
type wsFrame struct {
	Type         string    `json:"type"`
	ID           string    `json:"id"`
	SenderKey    string    `json:"sender_key"`
	EphemeralKey string    `json:"ephemeral_key"`
	Ciphertext   string    `json:"ciphertext"`
	CreatedAt    time.Time `json:"created_at"`
}

// listenWS connects to the server WebSocket and calls onMessage for each
// incoming message. Blocks until the connection drops or ctx is cancelled.
// The token parameter is the raw auth token (without "Ed25519 " prefix).
func listenWS(serverURL, token string, onMessage func(ServerMessage)) error {
	// Convert http → ws.
	wsURL := strings.Replace(serverURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL += "/v1/ws?auth=" + url.QueryEscape(token)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}
	defer conn.Close()

	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		conn.SetReadDeadline(time.Now().Add(70 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		var frame wsFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			continue
		}
		if frame.Type != "message" {
			continue
		}

		onMessage(ServerMessage{
			ID:           frame.ID,
			SenderKey:    frame.SenderKey,
			EphemeralKey: frame.EphemeralKey,
			Ciphertext:   frame.Ciphertext,
			CreatedAt:    frame.CreatedAt,
		})

		// Send ACK.
		ack, _ := json.Marshal(map[string]string{"type": "ack", "id": frame.ID})
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		conn.WriteMessage(websocket.TextMessage, ack)
	}
}

func readBody(r io.Reader) string {
	b, _ := io.ReadAll(r)
	return strings.TrimSpace(string(b))
}
