package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ctxKey is a private type used for context values to avoid collisions.
type ctxKey string

const identityCtxKey ctxKey = "identity"

// verifyAuthHeader parses and cryptographically verifies the custom Ed25519
// authorization header.
//
// Expected format:
//
//	Ed25519 <base64url_pubkey>.<unix_timestamp_seconds>.<base64url_signature>
//
// The signature covers "<base64url_pubkey>.<unix_timestamp_seconds>" as raw
// UTF-8 bytes. The timestamp must be within 30 seconds of server time.
func verifyAuthHeader(header string) (identityKey string, err error) {
	const prefix = "Ed25519 "
	if !strings.HasPrefix(header, prefix) {
		return "", errors.New("authorization header must start with 'Ed25519 '")
	}
	token := header[len(prefix):]

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("authorization token must have exactly 3 dot-separated parts")
	}

	pubKeyB64, tsStr, sigB64 := parts[0], parts[1], parts[2]

	// Decode public key.
	pubKeyBytes, err := base64.RawURLEncoding.DecodeString(pubKeyB64)
	if err != nil {
		return "", fmt.Errorf("invalid public key encoding: %w", err)
	}
	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return "", fmt.Errorf("public key must be %d bytes, got %d", ed25519.PublicKeySize, len(pubKeyBytes))
	}

	// Parse and validate timestamp.
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid timestamp: %w", err)
	}
	now := time.Now().Unix()
	diff := now - ts
	if diff < 0 {
		diff = -diff
	}
	if diff > 30 {
		return "", fmt.Errorf("timestamp is %d seconds from server time (max 30)", diff)
	}

	// Decode signature.
	sigBytes, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return "", fmt.Errorf("invalid signature encoding: %w", err)
	}

	// Verify signature over "<pubkey>.<timestamp>".
	message := pubKeyB64 + "." + tsStr
	pub := ed25519.PublicKey(pubKeyBytes)
	if !ed25519.Verify(pub, []byte(message), sigBytes) {
		return "", errors.New("signature verification failed")
	}

	return pubKeyB64, nil
}

// authMiddleware is an HTTP middleware that enforces Ed25519 token
// authentication. On success it stores the caller's identity key in the
// request context and calls next. On failure it returns 401.
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identityKey, err := verifyAuthHeader(r.Header.Get("Authorization"))
		if err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		ctx := context.WithValue(r.Context(), identityCtxKey, identityKey)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// identityFromCtx retrieves the authenticated identity key stored in the
// context by authMiddleware.
func identityFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(identityCtxKey).(string)
	return v
}
