package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// StoredMessage is the on-disk representation of a message.
type StoredMessage struct {
	ID       string    `json:"id"`
	Time     time.Time `json:"time"`
	Sender   string    `json:"sender"`
	Text     string    `json:"text"`
	Outgoing bool      `json:"outgoing"`
}

// storeMu serialises all file reads+writes so concurrent saves don't
// overwrite each other (e.g. an incoming and an outgoing message racing).
var storeMu sync.Mutex

func msgsPath(home, contactIdentityKey string) string {
	safe := strings.NewReplacer("/", "_", "+", "-").Replace(contactIdentityKey)
	return filepath.Join(home, "msgs_"+safe+".json")
}

func loadMessages(home, contactIdentityKey string) ([]StoredMessage, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	return readMessages(home, contactIdentityKey)
}

// readMessages is the unsynchronised inner read — call only while holding storeMu.
func readMessages(home, contactIdentityKey string) ([]StoredMessage, error) {
	path := msgsPath(home, contactIdentityKey)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var msgs []StoredMessage
	return msgs, json.Unmarshal(data, &msgs)
}

func appendStoredMessage(home, contactIdentityKey string, msg StoredMessage) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	existing, err := readMessages(home, contactIdentityKey)
	if err != nil {
		return err
	}
	for _, m := range existing {
		if m.ID == msg.ID {
			return nil // already saved
		}
	}
	existing = append(existing, msg)
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(msgsPath(home, contactIdentityKey), data, 0600)
}
