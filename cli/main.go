package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultServer = "http://localhost:8080"

func usage() {
	fmt.Print(`mcli — private encrypted messenger CLI

Usage: mcli [--home <dir>] <command> [args]

  --home <dir>   config directory (default: ~/.mcli or $MCLI_HOME)

Commands:
  init [--server <url>]        Generate keys and register with server
  whoami                       Print your identity key (share this with contacts)
  add <identity_key> [nick]    Add a contact by their identity key
  contacts                     List all contacts
  send <nick> <message>        Send an encrypted message
  recv                         Fetch and decrypt pending messages
  listen                       Listen for incoming messages via WebSocket
  chat <nick>                  Interactive bidirectional chat (Ctrl+C to exit)

Environment:
  MCLI_HOME   default config directory
`)
}

func main() {
	home := os.Getenv("MCLI_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".mcli")
	}

	args := os.Args[1:]

	// Parse --home flag anywhere in args.
	for i := 0; i < len(args); i++ {
		if args[i] == "--home" && i+1 < len(args) {
			home = args[i+1]
			args = append(args[:i], args[i+2:]...)
			break
		}
	}

	if len(args) == 0 {
		usage()
		os.Exit(1)
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "init":
		cmdInit(home, rest)
	case "whoami":
		cmdWhoami(home)
	case "add":
		cmdAdd(home, rest)
	case "contacts":
		cmdContacts(home)
	case "send":
		cmdSend(home, rest)
	case "recv":
		cmdRecv(home)
	case "listen":
		cmdListen(home)
	case "chat":
		cmdChat(home, rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %q\n\n", cmd)
		usage()
		os.Exit(1)
	}
}

// ── init ─────────────────────────────────────────────────────────────────────

func cmdInit(home string, args []string) {
	// Check existing config.
	existing, _ := loadConfig(home)
	if existing != nil {
		fmt.Println("already initialised. identity key:")
		fmt.Println(" ", identityKey(mustDecode64(existing.IdentityKey)))
		return
	}

	server := defaultServer
	for i, a := range args {
		if a == "--server" && i+1 < len(args) {
			server = args[i+1]
		}
	}

	sigPriv, agreePriv, err := generateKeys()
	if err != nil {
		fatalf("key generation failed: %v", err)
	}

	cfg := &Config{
		ServerURL:    server,
		IdentityKey:  base64.RawURLEncoding.EncodeToString(sigPriv),
		AgreementKey: base64.RawURLEncoding.EncodeToString(agreePriv),
	}
	if err := saveConfig(home, cfg); err != nil {
		fatalf("save config: %v", err)
	}

	identKey := identityKey(sigPriv)
	encKey := encryptionKey(agreePriv)

	header, err := authHeader(sigPriv)
	if err != nil {
		fatalf("auth header: %v", err)
	}

	if err := registerUser(server, identKey, encKey, header); err != nil {
		fmt.Fprintf(os.Stderr, "warning: server registration failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "keys saved locally — re-run init or register manually later\n")
	} else {
		fmt.Println("registered with server:", server)
	}

	fmt.Println("\nyour identity key (share this with contacts):")
	fmt.Println(" ", identKey)
	fmt.Println("\nconfig saved to:", home)
}

// ── whoami ────────────────────────────────────────────────────────────────────

func cmdWhoami(home string) {
	cfg := mustLoadConfig(home)
	sigPriv := mustDecode64(cfg.IdentityKey)
	fmt.Println(identityKey(sigPriv))
}

// ── add ───────────────────────────────────────────────────────────────────────

func cmdAdd(home string, args []string) {
	if len(args) == 0 {
		fatalf("usage: mcli add <identity_key> [nickname]")
	}
	cfg := mustLoadConfig(home)
	theirIdentKey := args[0]
	nick := ""
	if len(args) >= 2 {
		nick = strings.Join(args[1:], " ")
	}

	// Fetch their encryption key from server.
	user, err := getUser(cfg.ServerURL, theirIdentKey)
	if err != nil {
		fatalf("lookup failed: %v", err)
	}
	if user == nil {
		fatalf("user not found on server: %s", theirIdentKey)
	}
	if nick == "" {
		nick = theirIdentKey[:8] // default: first 8 chars of key
	}

	contacts, err := loadContacts(home)
	if err != nil {
		fatalf("load contacts: %v", err)
	}
	// Replace if exists.
	found := false
	for i, c := range contacts {
		if c.IdentityKey == theirIdentKey {
			contacts[i].EncryptionKey = user.EncryptionKey
			contacts[i].Nickname = nick
			found = true
			break
		}
	}
	if !found {
		contacts = append(contacts, Contact{
			IdentityKey:   theirIdentKey,
			EncryptionKey: user.EncryptionKey,
			Nickname:      nick,
		})
	}
	if err := saveContacts(home, contacts); err != nil {
		fatalf("save contacts: %v", err)
	}
	// Sync to server so other clients pick it up.
	sigPriv := mustDecode64(cfg.IdentityKey)
	if header, err := authHeader(sigPriv); err == nil {
		_ = upsertServerContact(cfg.ServerURL, theirIdentKey, nick, header)
	}
	fmt.Printf("added contact %q (%s...)\n", nick, theirIdentKey[:12])
}

// ── contacts ──────────────────────────────────────────────────────────────────

func cmdContacts(home string) {
	contacts, err := loadContacts(home)
	if err != nil {
		fatalf("load contacts: %v", err)
	}
	if len(contacts) == 0 {
		fmt.Println("no contacts yet — use: mcli add <identity_key> [nickname]")
		return
	}
	fmt.Printf("%-16s  %s\n", "NICKNAME", "IDENTITY KEY")
	fmt.Println(strings.Repeat("-", 70))
	for _, c := range contacts {
		fmt.Printf("%-16s  %s\n", c.Nickname, c.IdentityKey)
	}
}

// ── send ──────────────────────────────────────────────────────────────────────

func cmdSend(home string, args []string) {
	if len(args) < 2 {
		fatalf("usage: mcli send <nickname> <message>")
	}
	cfg := mustLoadConfig(home)
	nick := args[0]
	text := strings.Join(args[1:], " ")

	contacts, err := loadContacts(home)
	if err != nil {
		fatalf("load contacts: %v", err)
	}
	contact := findContact(contacts, nick)
	if contact == nil {
		fatalf("contact not found: %q", nick)
	}

	agreePriv := mustDecode64(cfg.AgreementKey)
	sigPriv := mustDecode64(cfg.IdentityKey)

	ephKey, ct, err := encrypt(agreePriv, contact.EncryptionKey, text)
	if err != nil {
		fatalf("encryption failed: %v", err)
	}
	selfEncKey := encryptionKey(agreePriv)
	senderEphKey, senderCT, err := encrypt(agreePriv, selfEncKey, text)
	if err != nil {
		fatalf("self-encryption failed: %v", err)
	}

	header, err := authHeader(sigPriv)
	if err != nil {
		fatalf("auth: %v", err)
	}

	if err := sendMessage(cfg.ServerURL, contact.IdentityKey, ephKey, ct, senderEphKey, senderCT, header); err != nil {
		fatalf("send failed: %v", err)
	}

	fmt.Printf("→ %s: %s\n", contact.Nickname, text)
}

// ── recv ──────────────────────────────────────────────────────────────────────

func cmdRecv(home string) {
	cfg := mustLoadConfig(home)
	sigPriv := mustDecode64(cfg.IdentityKey)
	agreePriv := mustDecode64(cfg.AgreementKey)

	header, err := authHeader(sigPriv)
	if err != nil {
		fatalf("auth: %v", err)
	}

	msgs, err := getPendingMessages(cfg.ServerURL, header)
	if err != nil {
		fatalf("fetch failed: %v", err)
	}
	if len(msgs) == 0 {
		fmt.Println("no pending messages")
		return
	}

	contacts, _ := loadContacts(home)

	for _, m := range msgs {
		text, err := decrypt(agreePriv, m.EphemeralKey, m.Ciphertext)
		if err != nil {
			fmt.Printf("[%s] <%s...> [decryption failed: %v]\n",
				m.CreatedAt.Local().Format("15:04:05"), m.SenderKey[:12], err)
			continue
		}
		sender := resolveSender(contacts, m.SenderKey)
		fmt.Printf("[%s] %s: %s\n", m.CreatedAt.Local().Format("15:04:05"), sender, text)
	}
}

// ── listen ────────────────────────────────────────────────────────────────────

func cmdListen(home string) {
	cfg := mustLoadConfig(home)
	sigPriv := mustDecode64(cfg.IdentityKey)
	agreePriv := mustDecode64(cfg.AgreementKey)
	contacts, _ := loadContacts(home)

	token, err := authToken(sigPriv)
	if err != nil {
		fatalf("auth: %v", err)
	}

	fmt.Printf("listening on %s — press Ctrl+C to exit\n", cfg.ServerURL)

	err = listenWS(cfg.ServerURL, token, func(m ServerMessage) {
		text, err := decrypt(agreePriv, m.EphemeralKey, m.Ciphertext)
		if err != nil {
			fmt.Printf("[%s] <%s...> [decryption failed: %v]\n",
				m.CreatedAt.Local().Format("15:04:05"), m.SenderKey[:12], err)
			return
		}
		sender := resolveSender(contacts, m.SenderKey)
		fmt.Printf("[%s] %s: %s\n", time.Now().Local().Format("15:04:05"), sender, text)
	})
	if err != nil {
		fatalf("websocket error: %v", err)
	}
}

// ── chat ──────────────────────────────────────────────────────────────────────

func cmdChat(home string, args []string) {
	if len(args) == 0 {
		fatalf("usage: mcli chat <nickname>")
	}
	cfg := mustLoadConfig(home)
	sigPriv := mustDecode64(cfg.IdentityKey)
	agreePriv := mustDecode64(cfg.AgreementKey)

	contacts, err := loadContacts(home)
	if err != nil {
		fatalf("load contacts: %v", err)
	}
	contact := findContact(contacts, args[0])
	if contact == nil {
		fatalf("contact not found: %q", args[0])
	}

	token, err := authToken(sigPriv)
	if err != nil {
		fatalf("auth: %v", err)
	}

	fmt.Printf("chatting with %s — type a message and press Enter (Ctrl+C to exit)\n\n", contact.Nickname)

	// WebSocket goroutine — print incoming.
	wsErr := make(chan error, 1)
	go func() {
		wsErr <- listenWS(cfg.ServerURL, token, func(m ServerMessage) {
			text, err := decrypt(agreePriv, m.EphemeralKey, m.Ciphertext)
			if err != nil {
				fmt.Printf("\r[%s] <%s...> [decryption failed]\n> ",
					time.Now().Local().Format("15:04:05"), m.SenderKey[:12])
				return
			}
			sender := resolveSender(contacts, m.SenderKey)
			fmt.Printf("\r[%s] %s: %s\n> ", time.Now().Local().Format("15:04:05"), sender, text)
		})
	}()

	// Stdin loop — send outgoing.
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			fmt.Print("> ")
			continue
		}

		header, err := authHeader(sigPriv)
		if err != nil {
			fmt.Fprintf(os.Stderr, "auth error: %v\n", err)
			fmt.Print("> ")
			continue
		}
		ephKey, ct, err := encrypt(agreePriv, contact.EncryptionKey, text)
		if err != nil {
			fmt.Fprintf(os.Stderr, "encryption error: %v\n", err)
			fmt.Print("> ")
			continue
		}
		selfEncKey := encryptionKey(agreePriv)
		senderEphKey, senderCT, err := encrypt(agreePriv, selfEncKey, text)
		if err != nil {
			fmt.Fprintf(os.Stderr, "self-encryption error: %v\n", err)
			fmt.Print("> ")
			continue
		}
		if err := sendMessage(cfg.ServerURL, contact.IdentityKey, ephKey, ct, senderEphKey, senderCT, header); err != nil {
			fmt.Fprintf(os.Stderr, "send error: %v\n", err)
		} else {
			fmt.Printf("[%s] you: %s\n", time.Now().Local().Format("15:04:05"), text)
		}
		fmt.Print("> ")
	}

	// Stdin closed — wait for WebSocket to also close.
	<-wsErr
}

// ── helpers ───────────────────────────────────────────────────────────────────

func mustDecode64(s string) []byte {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		fatalf("corrupt key in config: %v", err)
	}
	return b
}

func resolveSender(contacts []Contact, senderKey string) string {
	for _, c := range contacts {
		if c.IdentityKey == senderKey {
			return c.Nickname
		}
	}
	if len(senderKey) > 12 {
		return "<" + senderKey[:12] + "...>"
	}
	return "<" + senderKey + ">"
}
