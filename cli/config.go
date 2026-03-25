package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds the identity keys and server URL for this CLI instance.
type Config struct {
	ServerURL    string `json:"server_url"`
	IdentityKey  string `json:"identity_key"`  // base64url Ed25519 private key (64 bytes)
	AgreementKey string `json:"agreement_key"` // base64url X25519 private key (32 bytes)
}

// Contact represents a known peer.
type Contact struct {
	IdentityKey   string `json:"identity_key"`
	EncryptionKey string `json:"encryption_key"`
	Nickname      string `json:"nickname"`
}

// configPath returns the path to the config file inside home.
func configPath(home string) string { return filepath.Join(home, "config.json") }

// contactsPath returns the path to the contacts file inside home.
func contactsPath(home string) string { return filepath.Join(home, "contacts.json") }

// loadConfig reads the config from disk. Returns nil, nil if not found.
func loadConfig(home string) (*Config, error) {
	data, err := os.ReadFile(configPath(home))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	return &c, json.Unmarshal(data, &c)
}

// mustLoadConfig loads config or exits with a helpful message.
func mustLoadConfig(home string) *Config {
	cfg, err := loadConfig(home)
	if err != nil {
		fatalf("failed to read config: %v", err)
	}
	if cfg == nil {
		fatalf("not initialised — run: mcli init")
	}
	return cfg
}

// saveConfig writes the config to disk, creating the directory if needed.
func saveConfig(home string, cfg *Config) error {
	if err := os.MkdirAll(home, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(home), data, 0600)
}

// loadContacts reads the contacts list from disk.
func loadContacts(home string) ([]Contact, error) {
	data, err := os.ReadFile(contactsPath(home))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var contacts []Contact
	return contacts, json.Unmarshal(data, &contacts)
}

// saveContacts writes the contacts list to disk.
func saveContacts(home string, contacts []Contact) error {
	if err := os.MkdirAll(home, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(contacts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(contactsPath(home), data, 0600)
}

// findContact looks up a contact by nickname or identity key.
func findContact(contacts []Contact, query string) *Contact {
	for i := range contacts {
		if contacts[i].Nickname == query || contacts[i].IdentityKey == query {
			return &contacts[i]
		}
	}
	return nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
