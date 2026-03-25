import * as crypto from './crypto.js'
import * as storage from './storage.js'
import type { Contact, StoredMessage } from './storage.js'
import * as api from './api.js'
import type { ServerMessage } from './api.js'

// ─── State ────────────────────────────────────────────────────────────────────

interface AppState {
  signingPriv: Uint8Array | null
  agreementPriv: Uint8Array | null
  myIdentKey: string
  myEncKey: string
  contacts: Contact[]
  conversations: Record<string, StoredMessage[]>
  hasMoreHistory: Record<string, boolean>
  activeChatKey: string | null
  ws: WebSocket | null
  wsStatus: 'connected' | 'disconnected'
  wsReconnectTimer: ReturnType<typeof setTimeout> | null
  modal: 'add-contact' | 'settings' | null
  isMobile: boolean
}

const S: AppState = {
  signingPriv: null,
  agreementPriv: null,
  myIdentKey: '',
  myEncKey: '',
  contacts: [],
  conversations: {},
  hasMoreHistory: {},
  activeChatKey: null,
  ws: null,
  wsStatus: 'disconnected',
  wsReconnectTimer: null,
  modal: null,
  isMobile: false,
}

// ─── DOM helpers ──────────────────────────────────────────────────────────────

function $<T extends HTMLElement = HTMLElement>(id: string): T {
  return document.getElementById(id) as T
}

// ─── Bootstrap ────────────────────────────────────────────────────────────────

export function init(): void {
  S.isMobile = window.innerWidth <= 640

  const keys = storage.loadKeys()
  if (keys) {
    S.signingPriv = keys.signingPriv
    S.agreementPriv = keys.agreementPriv
    S.myIdentKey = crypto.identityKeyB64(S.signingPriv)
    S.myEncKey = crypto.encryptionKeyB64(S.agreementPriv)
    S.contacts = storage.loadContacts()
    for (const c of S.contacts) {
      S.conversations[c.identityKey] = storage.loadMessages(c.identityKey)
    }
    showMain()
    scheduleSync()
    connectWS()
  } else {
    showOnboarding()
  }

  bindEvents()
}

// ─── Onboarding ───────────────────────────────────────────────────────────────

function showOnboarding(): void {
  $('screen-ob').classList.remove('hidden')
  $('screen-main').classList.add('hidden')
  showObPanel('ob-choose')
}

function showObPanel(id: string): void {
  for (const p of ['ob-choose', 'ob-backup', 'ob-restore']) {
    $(p).classList.toggle('hidden', p !== id)
  }
}

async function handleGenerate(): Promise<void> {
  const keys = crypto.generateKeys()
  S.signingPriv = keys.signingPriv
  S.agreementPriv = keys.agreementPriv
  S.myIdentKey = crypto.identityKeyB64(S.signingPriv)
  S.myEncKey = crypto.encryptionKeyB64(S.agreementPriv)

  try {
    await api.registerUser(S.signingPriv, S.myIdentKey, S.myEncKey)
  } catch (e) {
    setObError(`Registration failed: ${(e as Error).message}`)
    return
  }

  storage.saveKeys(S.signingPriv, S.agreementPriv)
  $('backup-code').textContent = crypto.makeBackupCode(S.signingPriv, S.agreementPriv)
  showObPanel('ob-backup')
}

function handleBackupDone(): void {
  S.contacts = []
  S.conversations = {}
  showMain()
  scheduleSync()
  connectWS()
}

async function handleRestore(): Promise<void> {
  const code = $<HTMLInputElement>('restore-input').value.trim()
  setObError('')
  let keys: ReturnType<typeof crypto.parseBackupCode>
  try {
    keys = crypto.parseBackupCode(code)
  } catch (e) {
    setObError((e as Error).message)
    return
  }

  S.signingPriv = keys.signingPriv
  S.agreementPriv = keys.agreementPriv
  S.myIdentKey = crypto.identityKeyB64(S.signingPriv)
  S.myEncKey = crypto.encryptionKeyB64(S.agreementPriv)

  try {
    await api.registerUser(S.signingPriv, S.myIdentKey, S.myEncKey)
  } catch (e) {
    setObError(`Server error: ${(e as Error).message}`)
    return
  }

  storage.saveKeys(S.signingPriv, S.agreementPriv)
  S.contacts = storage.loadContacts()
  for (const c of S.contacts) {
    S.conversations[c.identityKey] = storage.loadMessages(c.identityKey)
  }
  showMain()
  scheduleSync()
  connectWS()
}

function setObError(msg: string): void {
  $('ob-error').textContent = msg
}

// ─── Main screen ──────────────────────────────────────────────────────────────

function showMain(): void {
  $('screen-ob').classList.add('hidden')
  $('screen-main').classList.remove('hidden')
  renderContacts()
  renderWsStatus()
}

// ─── Contact list ─────────────────────────────────────────────────────────────

function renderContacts(): void {
  const list = $('contact-list')
  if (S.contacts.length === 0) {
    list.innerHTML = '<div class="contact-empty">no contacts yet<br><span>click + to add one</span></div>'
    return
  }

  list.innerHTML = S.contacts.map(c => {
    const msgs = S.conversations[c.identityKey] ?? []
    const last = msgs[msgs.length - 1]
    const preview = last ? escHtml(last.text.slice(0, 60)) : ''
    const active = S.activeChatKey === c.identityKey ? 'active' : ''
    return `
      <div class="contact-item ${active}" data-key="${escAttr(c.identityKey)}">
        <div class="contact-name">${escHtml(c.nickname.toUpperCase())}</div>
        <div class="contact-key-short">${escHtml(c.identityKey.slice(0, 16))}…</div>
        ${preview ? `<div class="contact-preview">${preview}</div>` : ''}
      </div>`
  }).join('')
}

// ─── Chat view ────────────────────────────────────────────────────────────────

function openChat(contact: Contact): void {
  S.activeChatKey = contact.identityKey
  S.conversations[contact.identityKey] ??= storage.loadMessages(contact.identityKey)

  $('chat-empty').classList.add('hidden')
  $('chat-view').classList.remove('hidden')
  $('chat-header-name').textContent = contact.nickname.toUpperCase()
  $('chat-header-key').textContent = contact.identityKey.slice(0, 24) + '…'

  if (S.isMobile) {
    $('sidebar').classList.add('mobile-hidden')
    $('chat-area').classList.add('mobile-active')
  }

  renderContacts()
  renderMessages(true)
  $('message-input').focus()
}

function closeChat(): void {
  S.activeChatKey = null
  $('chat-empty').classList.remove('hidden')
  $('chat-view').classList.add('hidden')
  if (S.isMobile) {
    $('sidebar').classList.remove('mobile-hidden')
    $('chat-area').classList.remove('mobile-active')
  }
  renderContacts()
}

function renderMessages(scrollToBottom = false): void {
  if (!S.activeChatKey) return
  const msgs = S.conversations[S.activeChatKey] ?? []
  const hasMore = S.hasMoreHistory[S.activeChatKey] !== false

  const list = $('message-list')
  const prevScrollHeight = list.scrollHeight
  const prevScrollTop = list.scrollTop
  const wasAtBottom = prevScrollHeight - prevScrollTop - list.clientHeight < 40

  list.innerHTML = `
    ${hasMore ? `<div class="load-earlier"><button id="btn-load-earlier">load earlier messages</button></div>` : ''}
    ${msgs.map(m => `
      <div class="msg-row ${m.isOutgoing ? 'outgoing' : 'incoming'}">
        <div class="msg-bubble">
          <div class="msg-text">${escHtml(m.text)}</div>
          <div class="msg-time">${formatTime(m.createdAt)}</div>
        </div>
      </div>`).join('')}
  `

  document.getElementById('btn-load-earlier')?.addEventListener('click', loadOlderMessages)

  if (scrollToBottom) {
    list.scrollTop = list.scrollHeight
  } else if (wasAtBottom) {
    list.scrollTop = list.scrollHeight
  } else {
    list.scrollTop = prevScrollTop + (list.scrollHeight - prevScrollHeight)
  }
}

// ─── Send message ─────────────────────────────────────────────────────────────

async function sendMessage(): Promise<void> {
  if (!S.signingPriv || !S.agreementPriv || !S.activeChatKey) return
  const input = $<HTMLTextAreaElement>('message-input')
  const text = input.value.trim()
  if (!text) return

  const contact = S.contacts.find(c => c.identityKey === S.activeChatKey)
  if (!contact) return

  input.value = ''
  autoResizeInput()
  updateSendButton()
  setChatError('')

  try {
    const { ephemeralKey, ciphertext } = crypto.encrypt(S.agreementPriv, contact.encryptionKey, text)
    const { ephemeralKey: senderEphemeralKey, ciphertext: senderCiphertext } = crypto.encryptForSelf(S.agreementPriv, text)

    const resp = await api.sendMessage(S.signingPriv, {
      recipientKey: contact.identityKey,
      ephemeralKey,
      ciphertext,
      senderEphemeralKey,
      senderCiphertext,
    })

    addToConversation(S.activeChatKey, {
      id: resp.id,
      senderKey: S.myIdentKey,
      text,
      createdAt: resp.created_at,
      isOutgoing: true,
    })
  } catch (e) {
    setChatError((e as Error).message)
    input.value = text
    updateSendButton()
  }
}

// ─── Load older messages (pagination) ────────────────────────────────────────

async function loadOlderMessages(): Promise<void> {
  if (!S.signingPriv || !S.activeChatKey) return
  const beforeId = storage.oldestMessageId(S.activeChatKey)
  const limit = 50
  try {
    const raw = await api.getHistory(S.signingPriv, { limit, beforeId })
    processHistoryBatch([...raw].reverse(), true, S.activeChatKey)
    if (raw.length < limit) S.hasMoreHistory[S.activeChatKey] = false
    renderMessages()
  } catch (e) {
    console.error('[history] load older failed:', e)
  }
}

// ─── History sync ─────────────────────────────────────────────────────────────

function scheduleSync(): void {
  doSync()
  setInterval(doSync, 60_000)
}

async function doSync(): Promise<void> {
  await syncContacts()
  await syncHistory()
  await fetchPending()
}

async function syncContacts(): Promise<void> {
  if (!S.signingPriv) return
  try {
    const serverContacts = await api.getContacts(S.signingPriv)
    for (const sc of serverContacts) {
      const existing = S.contacts.find(c => c.identityKey === sc.contact_key)
      if (existing) {
        if (existing.nickname !== sc.nickname) existing.nickname = sc.nickname
      } else {
        try {
          const user = await api.getUser(sc.contact_key)
          if (user) {
            S.contacts.push({ identityKey: sc.contact_key, encryptionKey: user.encryption_key, nickname: sc.nickname })
            S.conversations[sc.contact_key] ??= storage.loadMessages(sc.contact_key)
          }
        } catch { /* skip unknown user */ }
      }
    }
    storage.saveContacts(S.contacts)
    renderContacts()
  } catch (e) {
    console.error('[sync] contacts failed:', e)
  }
}

async function syncHistory(): Promise<void> {
  if (!S.signingPriv) return
  const since = storage.newestMessageDate()
  try {
    const msgs = await api.getHistory(S.signingPriv, { limit: 200, since })
    processHistoryBatch(msgs)
  } catch (e) {
    console.error('[sync] history failed:', e)
  }
}

async function fetchPending(): Promise<void> {
  if (!S.signingPriv) return
  try {
    const msgs = await api.getPendingMessages(S.signingPriv)
    for (const m of msgs) handleIncoming(m)
  } catch (e) {
    console.error('[sync] pending failed:', e)
  }
}

function processHistoryBatch(msgs: ServerMessage[], prepend = false, targetKey?: string): void {
  if (!S.agreementPriv) return
  for (const m of msgs) {
    const isOutgoing = m.sender_key === S.myIdentKey
    const contactKey = isOutgoing ? m.recipient_key : m.sender_key
    if (!contactKey) continue
    if (targetKey && contactKey !== targetKey) continue

    let text: string
    try {
      if (isOutgoing) {
        if (!m.sender_ephemeral_key || !m.sender_ciphertext) continue
        text = crypto.decrypt(S.agreementPriv, m.sender_ephemeral_key, m.sender_ciphertext)
      } else {
        text = crypto.decrypt(S.agreementPriv, m.ephemeral_key, m.ciphertext)
      }
    } catch { continue }

    const stored: StoredMessage = { id: m.id, senderKey: m.sender_key, text, createdAt: m.created_at, isOutgoing }

    if (prepend) {
      storage.prependMessages(contactKey, [stored])
      const arr = S.conversations[contactKey] ?? []
      if (!arr.some(x => x.id === stored.id)) {
        S.conversations[contactKey] = [stored, ...arr]
      }
    } else {
      addToConversation(contactKey, stored)
    }
  }
  renderContacts()
  if (S.activeChatKey) renderMessages()
}

// ─── WebSocket ────────────────────────────────────────────────────────────────

function connectWS(): void {
  if (!S.signingPriv) return
  if (S.ws) { S.ws.onclose = null; S.ws.close(); S.ws = null }
  if (S.wsReconnectTimer) { clearTimeout(S.wsReconnectTimer); S.wsReconnectTimer = null }

  S.ws = api.openWebSocket(S.signingPriv, {
    onMessage: frame => handleIncoming(frame),
    onOpen: () => { S.wsStatus = 'connected'; renderWsStatus() },
    onClose: () => {
      S.wsStatus = 'disconnected'
      renderWsStatus()
      S.ws = null
      S.wsReconnectTimer = setTimeout(connectWS, 5000)
    },
  })
}

function handleIncoming(frame: ServerMessage): void {
  if (!S.agreementPriv || !S.signingPriv) return
  const senderKey = frame.sender_key
  const isEcho = senderKey === S.myIdentKey

  if (isEcho) {
    // Echo of a message we sent from another device — decrypt with sender copy.
    if (!frame.recipient_key || !frame.sender_ephemeral_key || !frame.sender_ciphertext) return
    let text: string
    try {
      text = crypto.decrypt(S.agreementPriv, frame.sender_ephemeral_key, frame.sender_ciphertext)
    } catch (e) {
      console.error('[ws] echo decrypt failed', e)
      return
    }
    addToConversation(frame.recipient_key, {
      id: frame.id,
      senderKey,
      text,
      createdAt: frame.created_at,
      isOutgoing: true,
    })
    return
  }

  // Incoming message from another user.
  let text: string
  try {
    text = crypto.decrypt(S.agreementPriv, frame.ephemeral_key, frame.ciphertext)
  } catch (e) {
    console.error('[ws] decrypt failed', e)
    return
  }

  if (!S.contacts.find(c => c.identityKey === senderKey)) {
    const nick = senderKey.slice(0, 8) + '…'
    S.contacts.push({ identityKey: senderKey, encryptionKey: '', nickname: nick })
    storage.saveContacts(S.contacts)
    S.conversations[senderKey] ??= []
    api.upsertContact(S.signingPriv, senderKey, nick).catch(() => {})
  }

  addToConversation(senderKey, {
    id: frame.id,
    senderKey,
    text,
    createdAt: frame.created_at,
    isOutgoing: false,
  })

  if (S.activeChatKey !== senderKey) showNotification(senderKey, text)
}

function showNotification(senderKey: string, text: string): void {
  if (Notification.permission !== 'granted') return
  const name = S.contacts.find(c => c.identityKey === senderKey)?.nickname ?? senderKey.slice(0, 8)
  const n = new Notification(name, { body: text, tag: senderKey })
  n.onclick = () => {
    window.focus()
    const c = S.contacts.find(x => x.identityKey === senderKey)
    if (c) openChat(c)
    n.close()
  }
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function addToConversation(contactKey: string, msg: StoredMessage): void {
  const arr = S.conversations[contactKey] ??= []
  if (arr.some(m => m.id === msg.id)) return
  arr.push(msg)
  storage.appendMessage(contactKey, msg)
  if (S.activeChatKey === contactKey) renderMessages()
  renderContacts()
}

function renderWsStatus(): void {
  $('ws-dot').className = 'ws-dot ' + S.wsStatus
  $('ws-label').textContent = S.wsStatus === 'connected' ? 'connected' : 'offline'
}

function setChatError(msg: string): void {
  const el = $('chat-error')
  el.textContent = msg
  el.classList.toggle('hidden', !msg)
}

function autoResizeInput(): void {
  const el = $<HTMLTextAreaElement>('message-input')
  el.style.height = 'auto'
  el.style.height = Math.min(el.scrollHeight, 120) + 'px'
}

function updateSendButton(): void {
  const has = $<HTMLTextAreaElement>('message-input').value.trim().length > 0
  $('btn-send').classList.toggle('can-send', has)
}

function formatTime(iso: string): string {
  return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

function escHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

function escAttr(s: string): string {
  return s.replace(/"/g, '&quot;')
}

// ─── Modals ───────────────────────────────────────────────────────────────────

function openModal(id: 'add-contact' | 'settings'): void {
  S.modal = id
  $('modal-overlay').classList.remove('hidden')
  $('modal-add-contact').classList.toggle('hidden', id !== 'add-contact')
  $('modal-settings').classList.toggle('hidden', id !== 'settings')
  if (id === 'settings') populateSettings()
}

function closeModal(): void {
  S.modal = null
  $('modal-overlay').classList.add('hidden')
}

function populateSettings(): void {
  $<HTMLInputElement>('settings-server-url').value = storage.getServerUrl()
  $('settings-identity-key').textContent = S.myIdentKey
  if (S.signingPriv && S.agreementPriv) {
    $('settings-backup-code').textContent = crypto.makeBackupCode(S.signingPriv, S.agreementPriv)
  }
}

async function handleAddContact(): Promise<void> {
  const key = $<HTMLInputElement>('add-contact-key').value.trim()
  const nick = $<HTMLInputElement>('add-contact-nick').value.trim()
  $('add-contact-error').textContent = ''

  if (!key) { $('add-contact-error').textContent = 'Identity key is required'; return }
  if (!nick) { $('add-contact-error').textContent = 'Nickname is required'; return }
  if (S.contacts.find(c => c.identityKey === key)) {
    $('add-contact-error').textContent = 'Contact already exists'
    return
  }

  let encKey = ''
  try {
    const user = await api.getUser(key)
    if (!user) { $('add-contact-error').textContent = 'User not found on server'; return }
    encKey = user.encryption_key
  } catch (e) {
    $('add-contact-error').textContent = (e as Error).message
    return
  }

  const contact: Contact = { identityKey: key, encryptionKey: encKey, nickname: nick }
  S.contacts.push(contact)
  S.conversations[key] = storage.loadMessages(key)
  storage.saveContacts(S.contacts)
  if (S.signingPriv) api.upsertContact(S.signingPriv, key, nick).catch(() => {})

  $<HTMLInputElement>('add-contact-key').value = ''
  $<HTMLInputElement>('add-contact-nick').value = ''
  closeModal()
  renderContacts()
}

function handleSaveSettings(): void {
  const url = $<HTMLInputElement>('settings-server-url').value.trim()
  if (url) storage.setServerUrl(url)
  closeModal()
  connectWS()
}

// ─── Event bindings ───────────────────────────────────────────────────────────

function bindEvents(): void {
  $('btn-generate').addEventListener('click', () => { void handleGenerate() })
  $('btn-restore').addEventListener('click', () => { setObError(''); showObPanel('ob-restore') })
  $('btn-backup-done').addEventListener('click', handleBackupDone)
  $('btn-restore-submit').addEventListener('click', () => { void handleRestore() })
  $<HTMLInputElement>('restore-input').addEventListener('keydown', e => { if (e.key === 'Enter') void handleRestore() })

  $('btn-add-contact').addEventListener('click', () => openModal('add-contact'))
  $('btn-settings').addEventListener('click', () => openModal('settings'))

  $('contact-list').addEventListener('click', e => {
    const item = (e.target as Element).closest<HTMLElement>('.contact-item')
    if (!item?.dataset['key']) return
    const contact = S.contacts.find(c => c.identityKey === item.dataset['key'])
    if (contact) openChat(contact)
  })

  $('btn-back').addEventListener('click', closeChat)

  $<HTMLTextAreaElement>('message-input').addEventListener('input', () => { autoResizeInput(); updateSendButton() })
  $<HTMLTextAreaElement>('message-input').addEventListener('keydown', e => {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); void sendMessage() }
  })
  $('btn-send').addEventListener('click', () => { void sendMessage() })

  $('modal-overlay').addEventListener('click', e => { if (e.target === $('modal-overlay')) closeModal() })
  $('btn-add-cancel').addEventListener('click', closeModal)
  $('btn-add-submit').addEventListener('click', () => { void handleAddContact() })
  $<HTMLInputElement>('add-contact-key').addEventListener('keydown', e => { if (e.key === 'Enter') $('add-contact-nick').focus() })
  $<HTMLInputElement>('add-contact-nick').addEventListener('keydown', e => { if (e.key === 'Enter') void handleAddContact() })
  $('btn-settings-close').addEventListener('click', closeModal)
  $('btn-settings-save').addEventListener('click', handleSaveSettings)

  document.addEventListener('click', () => {
    if (Notification.permission === 'default') void Notification.requestPermission()
  }, { once: true })

  window.addEventListener('resize', () => { S.isMobile = window.innerWidth <= 640 })
}
