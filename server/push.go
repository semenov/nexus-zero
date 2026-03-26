package main

import (
	"encoding/json"
	"log"
	"os"

	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/token"
)

// Pusher sends APNs push notifications. It is nil-safe: all methods are no-ops
// when the Pusher was not configured.
type Pusher struct {
	client   *apns2.Client
	bundleID string
}

// NewPusher creates a Pusher from environment variables:
//
//	APNS_KEY_PATH  — path to the .p8 private key file
//	APNS_KEY_ID    — 10-character key identifier
//	APNS_TEAM_ID   — 10-character team identifier
//	APNS_BUNDLE_ID — app bundle ID
//
// Returns nil (no-ops) when any variable is missing.
func NewPusher() *Pusher {
	keyPath := os.Getenv("APNS_KEY_PATH")
	keyID := os.Getenv("APNS_KEY_ID")
	teamID := os.Getenv("APNS_TEAM_ID")
	bundleID := os.Getenv("APNS_BUNDLE_ID")

	if keyPath == "" || keyID == "" || teamID == "" || bundleID == "" {
		log.Println("push: APNs not configured (APNS_* env vars missing) — push notifications disabled")
		return &Pusher{}
	}

	authKey, err := token.AuthKeyFromFile(keyPath)
	if err != nil {
		log.Printf("push: failed to load APNs key: %v", err)
		return &Pusher{}
	}

	t := &token.Token{
		AuthKey: authKey,
		KeyID:   keyID,
		TeamID:  teamID,
	}

	client := apns2.NewTokenClient(t).Production()
	log.Printf("push: APNs configured (bundle=%s key=%s)", bundleID, keyID)
	return &Pusher{client: client, bundleID: bundleID}
}

type apnsPayload struct {
	APS apnsAPS `json:"aps"`
}

type apnsAPS struct {
	Alert apnsAlert `json:"alert"`
	Sound string    `json:"sound"`
	Badge int       `json:"badge"`
}

type apnsAlert struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Send sends a push notification to a single device token.
func (p *Pusher) Send(deviceToken string) {
	if p.client == nil {
		return
	}

	payload, _ := json.Marshal(apnsPayload{
		APS: apnsAPS{
			Alert: apnsAlert{
				Title: "Nexus Zero",
				Body:  "New message",
			},
			Sound: "default",
			Badge: 1,
		},
	})

	n := &apns2.Notification{
		DeviceToken: deviceToken,
		Topic:       p.bundleID,
		Payload:     payload,
	}

	res, err := p.client.Push(n)
	if err != nil {
		log.Printf("push: APNs error for token %s: %v", deviceToken[:8], err)
		return
	}
	if !res.Sent() {
		log.Printf("push: APNs rejected token %s: %s", deviceToken[:8], res.Reason)
	}
}

// SendToTokens sends a push notification to multiple device tokens.
func (p *Pusher) SendToTokens(tokens []string) {
	for _, t := range tokens {
		go p.Send(t)
	}
}
