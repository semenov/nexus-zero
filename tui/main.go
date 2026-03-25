package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ── Types ────────────────────────────────────────────────────────────────────

type DisplayMessage struct {
	Time     time.Time
	Sender   string // "you" or contact nickname
	Text     string
	Outgoing bool
	System   bool
}

type App struct {
	cfg       *Config
	home      string
	sigPriv   []byte
	agreePriv []byte
	contacts  []Contact

	mu        sync.RWMutex
	messages  map[string][]DisplayMessage // contact identity key → history
	unread    map[string]int
	activeKey string // identity key of the currently visible contact

	// tview primitives
	tapp        *tview.Application
	contactList *tview.List
	chatView    *tview.TextView
	inputField  *tview.InputField
	statusBar   *tview.TextView
	chatTitle   *tview.TextView
}

// ── Entry point ───────────────────────────────────────────────────────────────

func main() {
	home := os.Getenv("MCLI_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".mcli")
	}
	for i, arg := range os.Args[1:] {
		if arg == "--home" && i+1 < len(os.Args[1:]) {
			home = os.Args[i+2]
		}
	}

	cfg := mustLoadConfig(home)
	contacts, _ := loadContacts(home)

	a := &App{
		cfg:       cfg,
		home:      home,
		sigPriv:   mustDecode64(cfg.IdentityKey),
		agreePriv: mustDecode64(cfg.AgreementKey),
		contacts:  contacts,
		messages:  make(map[string][]DisplayMessage),
		unread:    make(map[string]int),
	}

	a.run()
}

// ── UI build ──────────────────────────────────────────────────────────────────

func (a *App) run() {
	a.tapp = tview.NewApplication()

	// Status bar (top). Written directly before Run(); via QueueUpdateDraw after.
	a.statusBar = tview.NewTextView().SetDynamicColors(true)
	a.statusBar.SetBackgroundColor(tcell.ColorDarkBlue)

	// Contact list (left).
	a.contactList = tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true).
		SetWrapAround(true)
	a.contactList.
		SetBorder(true).
		SetTitle(" Contacts ").
		SetTitleAlign(tview.AlignLeft).
		SetBorderColor(tcell.ColorSlateGray)

	// Chat title bar.
	a.chatTitle = tview.NewTextView().SetDynamicColors(true)
	a.chatTitle.SetBackgroundColor(tcell.ColorDarkSlateGray)

	// Chat view (right). No SetChangedFunc — avoids QueueUpdate before Run().
	a.chatView = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(true)
	a.chatView.SetBorder(false)

	// Input field (bottom).
	a.inputField = tview.NewInputField().
		SetLabel(" ❯ ").
		SetLabelColor(tcell.ColorLimeGreen).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetFieldTextColor(tcell.ColorWhite)
	a.inputField.
		SetBorder(true).
		SetBorderColor(tcell.ColorSlateGray).
		SetTitle(" Message (Tab = contacts, Esc = contacts, Ctrl+C = quit) ").
		SetTitleAlign(tview.AlignLeft)

	// Load persisted message history before drawing the contact list so that
	// selectContactDirect → redrawChat sees the messages immediately.
	a.loadHistory()

	// Populate list and set initial state — all direct writes, no QueueUpdateDraw.
	a.populateContactList()
	if len(a.contacts) > 0 {
		a.selectContactDirect(0)
	} else {
		a.writeChatTitle("")
		fmt.Fprintf(a.chatView, "[gray]No contacts yet.\n\nUse the CLI to add one:\n  [white]mcli add <identity_key> <nickname>[gray]\n\nThen restart the TUI.\n")
	}
	a.writeStatusDirect("connecting…", false)

	// Layout.
	rightPanel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.chatTitle, 1, 0, false).
		AddItem(a.chatView, 0, 1, false).
		AddItem(a.inputField, 3, 0, true)

	mainArea := tview.NewFlex().
		AddItem(a.contactList, 28, 0, true).
		AddItem(rightPanel, 0, 1, false)

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.statusBar, 1, 0, false).
		AddItem(mainArea, 0, 1, true)

	a.tapp.SetRoot(root, true).SetFocus(a.contactList)

	a.setupInputHandler()
	a.setupKeyBindings()

	// Restore terminal on SIGINT/SIGTERM so the shell isn't left in raw mode.
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		<-ch
		a.tapp.Stop()
	}()

	// Start background goroutines BEFORE Run(). They will block on
	// QueueUpdateDraw until the event loop starts, then unblock automatically.
	go a.connectWS()
	go a.fetchHistory()
	go a.fetchPending()
	go a.syncContacts()

	if err := a.tapp.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tui:", err)
		os.Exit(1)
	}
}

// ── Status bar ────────────────────────────────────────────────────────────────

// writeStatusDirect writes the status bar without QueueUpdateDraw.
// Safe to call before app.Run().
func (a *App) writeStatusDirect(status string, connected bool) {
	a.statusBar.Clear()
	dot := "[red]●[-]"
	if connected {
		dot = "[green]●[-]"
	}
	idKey := identityKey(a.sigPriv)
	fmt.Fprintf(a.statusBar,
		" [white::b]Messenger[-:-:-]  %s [white]%s[-]  [gray]│  %s  │  you: %s[-]",
		dot, status, a.cfg.ServerURL, shortKey(idKey),
	)
}

// setStatus updates the status bar safely from any goroutine after Run().
func (a *App) setStatus(status string, connected bool) {
	a.tapp.QueueUpdateDraw(func() {
		a.writeStatusDirect(status, connected)
	})
}

// ── Contact list ──────────────────────────────────────────────────────────────

func (a *App) populateContactList() {
	a.contactList.Clear()
	for i, c := range a.contacts {
		idx := i
		a.contactList.AddItem(c.Nickname, shortKey(c.IdentityKey), 0, func() {
			a.selectContactDirect(idx)
			a.tapp.SetFocus(a.inputField)
		})
	}
	a.contactList.SetChangedFunc(func(index int, _ string, _ string, _ rune) {
		a.selectContactDirect(index)
	})
}

// selectContactDirect switches the active contact and refreshes the chat view.
// Safe to call before or after Run() (does not use QueueUpdateDraw).
func (a *App) selectContactDirect(index int) {
	if index < 0 || index >= len(a.contacts) {
		return
	}
	contact := a.contacts[index]
	a.activeKey = contact.IdentityKey

	a.mu.Lock()
	a.unread[contact.IdentityKey] = 0
	a.mu.Unlock()

	a.refreshContactLabels()
	a.writeChatTitle(contact.Nickname)
	a.redrawChat()
}

func (a *App) refreshContactLabels() {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for i, c := range a.contacts {
		label := c.Nickname
		if n := a.unread[c.IdentityKey]; n > 0 {
			label = fmt.Sprintf("[red]%s (%d)[-]", c.Nickname, n)
		}
		a.contactList.SetItemText(i, label, shortKey(c.IdentityKey))
	}
}

// ── Chat view ─────────────────────────────────────────────────────────────────

func (a *App) writeChatTitle(nick string) {
	a.chatTitle.Clear()
	if nick == "" {
		fmt.Fprintf(a.chatTitle, "  [gray]select a contact to start chatting[-]")
		return
	}
	fmt.Fprintf(a.chatTitle, "  [white::b]%s[-:-:-]", nick)
}

func (a *App) redrawChat() {
	a.chatView.Clear()
	a.mu.RLock()
	msgs := append([]DisplayMessage(nil), a.messages[a.activeKey]...)
	a.mu.RUnlock()

	if len(msgs) == 0 {
		fmt.Fprintf(a.chatView, "[gray]No messages yet — say hello!\n")
		return
	}
	for _, m := range msgs {
		writeMsg(a.chatView, m)
	}
	a.chatView.ScrollToEnd()
}

func writeMsg(w *tview.TextView, m DisplayMessage) {
	ts := fmt.Sprintf("[gray]%s[-]", m.Time.Local().Format("15:04"))
	switch {
	case m.System:
		fmt.Fprintf(w, "%s  [red]⚠ %s[-]\n", ts, tview.Escape(m.Text))
	case m.Outgoing:
		fmt.Fprintf(w, "%s  [green]you[-]: %s\n", ts, tview.Escape(m.Text))
	default:
		fmt.Fprintf(w, "%s  [yellow]%s[-]: %s\n", ts, tview.Escape(m.Sender), tview.Escape(m.Text))
	}
}

// ── Input & send ──────────────────────────────────────────────────────────────

func (a *App) setupInputHandler() {
	a.inputField.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter {
			return
		}
		text := strings.TrimSpace(a.inputField.GetText())
		if text == "" || a.activeKey == "" {
			return
		}
		a.inputField.SetText("")

		contact := a.findContactByKey(a.activeKey)
		if contact == nil {
			return
		}
		// Capture for goroutine.
		targetKey := a.activeKey
		targetEncKey := contact.EncryptionKey
		targetIdentKey := contact.IdentityKey

		go func() {
			ephKey, ct, err := encrypt(a.agreePriv, targetEncKey, text)
			if err != nil {
				a.queueAppend(targetKey, DisplayMessage{Time: time.Now(), Text: "encryption error: " + err.Error(), System: true})
				return
			}
			selfEncKey := encryptionKey(a.agreePriv)
			senderEphKey, senderCT, err := encrypt(a.agreePriv, selfEncKey, text)
			if err != nil {
				a.queueAppend(targetKey, DisplayMessage{Time: time.Now(), Text: "self-encrypt error: " + err.Error(), System: true})
				return
			}
			header, err := authHeader(a.sigPriv)
			if err != nil {
				a.queueAppend(targetKey, DisplayMessage{Time: time.Now(), Text: "auth error: " + err.Error(), System: true})
				return
			}
			resp, err := sendMessage(a.cfg.ServerURL, targetIdentKey, ephKey, ct, senderEphKey, senderCT, header)
			if err != nil {
				a.queueAppend(targetKey, DisplayMessage{Time: time.Now(), Text: "send error: " + err.Error(), System: true})
				return
			}
			dm := DisplayMessage{
				Time:     resp.CreatedAt,
				Sender:   "you",
				Text:     text,
				Outgoing: true,
			}
			a.queueAppend(targetKey, dm)
		}()
	})
}

// ── Key bindings ──────────────────────────────────────────────────────────────

func (a *App) setupKeyBindings() {
	a.tapp.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			if a.tapp.GetFocus() == a.inputField {
				a.tapp.SetFocus(a.contactList)
			} else {
				a.tapp.SetFocus(a.inputField)
			}
			return nil
		case tcell.KeyEscape:
			a.tapp.SetFocus(a.contactList)
			return nil
		case tcell.KeyRune:
			// Typing a printable character while the contact list has focus
			// auto-jumps to the input field and prepends the character.
			if a.tapp.GetFocus() == a.contactList {
				current := a.inputField.GetText()
				a.inputField.SetText(current + string(event.Rune()))
				a.tapp.SetFocus(a.inputField)
				return nil
			}
		}
		return event
	})
}

// ── WebSocket ─────────────────────────────────────────────────────────────────

func (a *App) connectWS() {
	backoff := time.Second
	for {
		token, err := authToken(a.sigPriv)
		if err != nil {
			a.setStatus("auth error", false)
			return
		}

		a.setStatus("connecting…", false)

		err = listenWS(a.cfg.ServerURL, token, func() {
			a.setStatus("connected", true)
		}, func(msg ServerMessage) {
			a.tapp.QueueUpdateDraw(func() {
				a.handleIncoming(msg)
			})
		})

		msg := fmt.Sprintf("reconnecting in %v…", backoff)
		a.setStatus(msg, false)
		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (a *App) handleIncoming(msg ServerMessage) {
	text, err := decrypt(a.agreePriv, msg.EphemeralKey, msg.Ciphertext)
	if err != nil {
		return
	}

	// Auto-add unknown senders so their messages are visible.
	if a.findContactByKey(msg.SenderKey) == nil {
		nick := msg.SenderKey
		if len(nick) > 8 {
			nick = nick[:8] + "…"
		}
		a.contacts = append(a.contacts, Contact{
			Nickname:      nick,
			IdentityKey:   msg.SenderKey,
			EncryptionKey: "", // unknown until they're added properly
		})
		a.populateContactList()
	}

	sender := a.resolveSender(msg.SenderKey)
	dm := DisplayMessage{
		Time:   msg.CreatedAt,
		Sender: sender,
		Text:   text,
	}

	a.mu.Lock()
	a.messages[msg.SenderKey] = append(a.messages[msg.SenderKey], dm)
	if msg.SenderKey != a.activeKey {
		a.unread[msg.SenderKey]++
	}
	a.mu.Unlock()

	go a.saveMessage(msg.SenderKey, dm)

	if msg.SenderKey == a.activeKey {
		writeMsg(a.chatView, dm)
		a.chatView.ScrollToEnd()
	}

	a.refreshContactLabels()
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// loadHistory reads persisted messages from disk into the in-memory store.
// Called once before Run(), so no QueueUpdateDraw needed.
func (a *App) loadHistory() {
	for _, c := range a.contacts {
		stored, err := loadMessages(a.home, c.IdentityKey)
		if err != nil || len(stored) == 0 {
			continue
		}
		msgs := make([]DisplayMessage, len(stored))
		for i, s := range stored {
			msgs[i] = DisplayMessage{
				Time:     s.Time,
				Sender:   s.Sender,
				Text:     s.Text,
				Outgoing: s.Outgoing,
			}
		}
		a.messages[c.IdentityKey] = msgs
	}
}

// syncContacts pulls the server-side contact list and merges it with local contacts.
func (a *App) syncContacts() {
	header, err := authHeader(a.sigPriv)
	if err != nil {
		return
	}
	serverContacts, err := getServerContacts(a.cfg.ServerURL, header)
	if err != nil {
		return
	}

	changed := false
	for _, sc := range serverContacts {
		found := false
		for _, lc := range a.contacts {
			if lc.IdentityKey == sc.ContactKey {
				found = true
				break
			}
		}
		if !found {
			// New contact from server — fetch encryption key.
			user, err := getUser(a.cfg.ServerURL, sc.ContactKey)
			if err != nil || user == nil {
				continue
			}
			a.contacts = append(a.contacts, Contact{
				Nickname:      sc.Nickname,
				IdentityKey:   sc.ContactKey,
				EncryptionKey: user.EncryptionKey,
			})
			changed = true
		}
	}
	if changed {
		_ = saveContacts(a.home, a.contacts)
		a.tapp.QueueUpdateDraw(func() {
			a.populateContactList()
			a.refreshContactLabels()
		})
	}
}

// fetchHistory loads server-side message history and merges it with the local
// in-memory store. On startup it performs an incremental sync: only messages
// newer than the newest locally stored message are fetched.
func (a *App) fetchHistory() {
	header, err := authHeader(a.sigPriv)
	if err != nil {
		return
	}

	// Find the newest locally stored message timestamp across all contacts.
	var newest *time.Time
	for _, c := range a.contacts {
		stored, _ := loadMessages(a.home, c.IdentityKey)
		for _, m := range stored {
			if newest == nil || m.Time.After(*newest) {
				t := m.Time
				newest = &t
			}
		}
	}

	msgs, err := getMessageHistory(a.cfg.ServerURL, header, 200, newest, nil)
	if err != nil || len(msgs) == 0 {
		return
	}

	myKey := identityKey(a.sigPriv)

	a.tapp.QueueUpdateDraw(func() {
		for _, m := range msgs {
			isOutgoing := m.SenderKey == myKey

			var contactKey string
			var text string
			if isOutgoing {
				if m.SenderEphemeralKey == nil || m.SenderCiphertext == nil {
					continue
				}
				t, err := decrypt(a.agreePriv, *m.SenderEphemeralKey, *m.SenderCiphertext)
				if err != nil {
					continue
				}
				text = t
				contactKey = m.RecipientKey
			} else {
				t, err := decrypt(a.agreePriv, m.EphemeralKey, m.Ciphertext)
				if err != nil {
					continue
				}
				text = t
				contactKey = m.SenderKey
			}

			sender := a.resolveSender(m.SenderKey)
			dm := DisplayMessage{
				Time:     m.CreatedAt,
				Sender:   sender,
				Text:     text,
				Outgoing: isOutgoing,
			}

			a.mu.Lock()
			// Deduplicate: skip if this timestamp+text already present locally.
			existing := a.messages[contactKey]
			alreadyHave := false
			for _, e := range existing {
				if e.Time.Equal(dm.Time) && e.Text == dm.Text {
					alreadyHave = true
					break
				}
			}
			if !alreadyHave {
				a.messages[contactKey] = append(existing, dm)
			}
			a.mu.Unlock()
		}

		// Sort messages by time for each contact.
		a.mu.Lock()
		for k, msgs := range a.messages {
			sorted := make([]DisplayMessage, len(msgs))
			copy(sorted, msgs)
			for i := 1; i < len(sorted); i++ {
				for j := i; j > 0 && sorted[j].Time.Before(sorted[j-1].Time); j-- {
					sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
				}
			}
			a.messages[k] = sorted
		}
		a.mu.Unlock()

		a.redrawChat()
	})
}

// fetchPending fetches messages that arrived while offline and delivers them.
func (a *App) fetchPending() {
	header, err := authHeader(a.sigPriv)
	if err != nil {
		return
	}
	msgs, err := getPendingMessages(a.cfg.ServerURL, header)
	if err != nil || len(msgs) == 0 {
		return
	}
	a.tapp.QueueUpdateDraw(func() {
		for _, m := range msgs {
			a.handleIncoming(m)
		}
	})
}

// saveMessage persists a display message to disk for a contact.
func (a *App) saveMessage(contactKey string, dm DisplayMessage) {
	id := fmt.Sprintf("%d", dm.Time.UnixNano())
	_ = appendStoredMessage(a.home, contactKey, StoredMessage{
		ID:       id,
		Time:     dm.Time,
		Sender:   dm.Sender,
		Text:     dm.Text,
		Outgoing: dm.Outgoing,
	})
}

// queueAppend adds a message, persists it, and refreshes the chat if active.
func (a *App) queueAppend(contactKey string, dm DisplayMessage) {
	a.tapp.QueueUpdateDraw(func() {
		a.mu.Lock()
		a.messages[contactKey] = append(a.messages[contactKey], dm)
		a.mu.Unlock()

		go a.saveMessage(contactKey, dm)

		if contactKey == a.activeKey {
			writeMsg(a.chatView, dm)
			a.chatView.ScrollToEnd()
		}
	})
}

func (a *App) findContactByKey(key string) *Contact {
	for i := range a.contacts {
		if a.contacts[i].IdentityKey == key {
			return &a.contacts[i]
		}
	}
	return nil
}

func (a *App) resolveSender(senderKey string) string {
	for _, c := range a.contacts {
		if c.IdentityKey == senderKey {
			return c.Nickname
		}
	}
	return shortKey(senderKey)
}

func shortKey(key string) string {
	if len(key) > 16 {
		return key[:16] + "…"
	}
	return key
}

func mustDecode64(s string) []byte {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		fatalf("corrupt key in config: %v", err)
	}
	return b
}
