package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

// b64 is shorthand for base64url without padding.
var b64 = base64.RawURLEncoding

// generateKeys generates a new Ed25519 signing keypair and an X25519 agreement keypair.
// Returns (signingPriv [64 bytes], agreementPriv [32 bytes]).
func generateKeys() (signingPriv, agreementPriv []byte, err error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate signing key: %w", err)
	}
	var agreePriv [32]byte
	if _, err := io.ReadFull(rand.Reader, agreePriv[:]); err != nil {
		return nil, nil, fmt.Errorf("generate agreement key: %w", err)
	}
	return priv, agreePriv[:], nil
}

// identityKey returns the base64url-encoded Ed25519 public key.
func identityKey(signingPriv []byte) string {
	pub := ed25519.PrivateKey(signingPriv).Public().(ed25519.PublicKey)
	return b64.EncodeToString(pub)
}

// encryptionKey returns the base64url-encoded X25519 public key.
func encryptionKey(agreementPriv []byte) string {
	pub, _ := curve25519.X25519(agreementPriv, curve25519.Basepoint)
	return b64.EncodeToString(pub)
}

// authToken returns the raw token string (without "Ed25519 " prefix) suitable
// for the ?auth= WebSocket query parameter.
func authToken(signingPriv []byte) (string, error) {
	pub := ed25519.PrivateKey(signingPriv).Public().(ed25519.PublicKey)
	pubB64 := b64.EncodeToString(pub)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	msg := pubB64 + "." + ts
	sig := ed25519.Sign(ed25519.PrivateKey(signingPriv), []byte(msg))
	return pubB64 + "." + ts + "." + b64.EncodeToString(sig), nil
}

// authHeader returns the full "Ed25519 <token>" Authorization header value.
func authHeader(signingPriv []byte) (string, error) {
	token, err := authToken(signingPriv)
	if err != nil {
		return "", err
	}
	return "Ed25519 " + token, nil
}

// encrypt encrypts plaintext for a recipient identified by their base64url X25519 public key.
// Returns base64url-encoded (ephemeralPublicKey, combinedCiphertext).
// combinedCiphertext = nonce (12 bytes) + ciphertext + tag (16 bytes).
func encrypt(agreementPriv []byte, recipientEncKey, text string) (ephKey, ciphertext string, err error) {
	recipientPub, err := b64.DecodeString(recipientEncKey)
	if err != nil {
		return "", "", fmt.Errorf("invalid recipient encryption key: %w", err)
	}

	// Ephemeral X25519 keypair.
	var ephPriv [32]byte
	if _, err := io.ReadFull(rand.Reader, ephPriv[:]); err != nil {
		return "", "", fmt.Errorf("generate ephemeral key: %w", err)
	}

	// ECDH.
	sharedSecret, err := curve25519.X25519(ephPriv[:], recipientPub)
	if err != nil {
		return "", "", fmt.Errorf("ECDH: %w", err)
	}

	// HKDF-SHA256 matching iOS CryptoKit parameters.
	kdf := hkdf.New(sha256.New, sharedSecret, []byte("messenger-v1"), []byte("message"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(kdf, key); err != nil {
		return "", "", fmt.Errorf("HKDF: %w", err)
	}

	// ChaCha20-Poly1305: prepend nonce to ciphertext+tag.
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return "", "", fmt.Errorf("chacha20poly1305: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", "", fmt.Errorf("generate nonce: %w", err)
	}
	combined := aead.Seal(nonce, nonce, []byte(text), nil)

	// Ephemeral public key.
	ephPub, _ := curve25519.X25519(ephPriv[:], curve25519.Basepoint)

	return b64.EncodeToString(ephPub), b64.EncodeToString(combined), nil
}

// decrypt decrypts a message using our X25519 private key and the sender's ephemeral public key.
func decrypt(agreementPriv []byte, ephKey, ciphertext string) (string, error) {
	ephPub, err := b64.DecodeString(ephKey)
	if err != nil {
		return "", fmt.Errorf("invalid ephemeral key: %w", err)
	}
	combined, err := b64.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("invalid ciphertext: %w", err)
	}

	// ECDH.
	sharedSecret, err := curve25519.X25519(agreementPriv, ephPub)
	if err != nil {
		return "", fmt.Errorf("ECDH: %w", err)
	}

	// HKDF.
	kdf := hkdf.New(sha256.New, sharedSecret, []byte("messenger-v1"), []byte("message"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(kdf, key); err != nil {
		return "", fmt.Errorf("HKDF: %w", err)
	}

	// ChaCha20-Poly1305.
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return "", fmt.Errorf("chacha20poly1305: %w", err)
	}
	if len(combined) < aead.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce := combined[:aead.NonceSize()]
	ct := combined[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed: %w", err)
	}
	return string(plaintext), nil
}
