import * as crypto from './crypto.js'
import * as storage from './storage.js'
import type { Nexus, NexusMember, StoredMessage } from './storage.js'
import * as api from './api.js'
import type { ServerMessage, MemberEvent } from './api.js'

// ─── State ──────────────────────────────────────────────────────────────────

interface AppState {
  signingPriv: Uint8Array | null
  agreementPriv: Uint8Array | null
  myIdentKey: string
  myEncKey: string
  username: string | null
  nexuses: Nexus[]
  conversations: Record<string, StoredMessage[]>
  hasMoreHistory: Record<string, boolean>
  activeNexusId: string | null
  ws: WebSocket | null
  wsStatus: 'connected' | 'disconnected'
  wsReconnectTimer: ReturnType<typeof setTimeout> | null
  modal: 'create-nexus' | 'join-nexus' | 'nexus-settings' | 'settings' | null
  isMobile: boolean
}

const S: AppState = {
  signingPriv: null,
  agreementPriv: null,
  myIdentKey: '',
  myEncKey: '',
  username: null,
  nexuses: [],
  conversations: {},
  hasMoreHistory: {},
  activeNexusId: null,
  ws: null,
  wsStatus: 'disconnected',
  wsReconnectTimer: null,
  modal: null,
  isMobile: false,
}

// ─── DOM helpers ────────────────────────────────────────────────────────────

function $<T extends HTMLElement = HTMLElement>(id: string): T {
  return document.getElementById(id) as T
}

// ─── Bootstrap ──────────────────────────────────────────────────────────────

export function init(): void {
  S.isMobile = window.innerWidth <= 640

  const keys = storage.loadKeys()
  if (keys) {
    S.signingPriv = keys.signingPriv
    S.agreementPriv = keys.agreementPriv
    S.myIdentKey = crypto.identityKeyB64(S.signingPriv)
    S.myEncKey = crypto.encryptionKeyB64(S.agreementPriv)
    S.username = storage.loadUsername()
    S.nexuses = storage.loadNexuses()
    for (const n of S.nexuses) {
      S.conversations[n.id] = storage.loadMessages(n.id)
    }
    if (!S.username) {
      showUsernameSetup()
    } else {
      showMain()
      scheduleSync()
      connectWS()
    }
  } else {
    showOnboarding()
  }

  bindEvents()
}

// ─── Onboarding ─────────────────────────────────────────────────────────────

function showOnboarding(): void {
  $('screen-ob').classList.remove('hidden')
  $('screen-main').classList.add('hidden')
  $('screen-username').classList.add('hidden')
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
  S.nexuses = []
  S.conversations = {}
  showUsernameSetup()
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

  // Try to load existing username from server.
  try {
    const user = await api.getUser(S.myIdentKey)
    if (user?.username) {
      S.username = user.username
      storage.saveUsername(S.username)
    }
  } catch { /* ignore */ }

  if (S.username) {
    S.nexuses = storage.loadNexuses()
    for (const n of S.nexuses) {
      S.conversations[n.id] = storage.loadMessages(n.id)
    }
    showMain()
    scheduleSync()
    connectWS()
  } else {
    showUsernameSetup()
  }
}

function setObError(msg: string): void {
  $('ob-error').textContent = msg
}

// ─── Username setup ─────────────────────────────────────────────────────────

function showUsernameSetup(): void {
  $('screen-ob').classList.add('hidden')
  $('screen-main').classList.add('hidden')
  $('screen-username').classList.remove('hidden')
}

async function handleSetUsername(): Promise<void> {
  if (!S.signingPriv) return
  const input = $<HTMLInputElement>('username-input')
  const username = input.value.trim()
  $('username-error').textContent = ''

  if (username.length < 2 || username.length > 32) {
    $('username-error').textContent = 'Username must be 2-32 characters'
    return
  }

  try {
    await api.setUsername(S.signingPriv, username)
  } catch (e) {
    $('username-error').textContent = (e as Error).message
    return
  }

  S.username = username
  storage.saveUsername(username)
  showMain()
  scheduleSync()
  connectWS()
}

// ─── Main screen ────────────────────────────────────────────────────────────

function showMain(): void {
  $('screen-ob').classList.add('hidden')
  $('screen-username').classList.add('hidden')
  $('screen-main').classList.remove('hidden')
  renderNexusList()
  renderWsStatus()
}

// ─── Nexus list ─────────────────────────────────────────────────────────────

function renderNexusList(): void {
  const list = $('nexus-list')
  if (S.nexuses.length === 0) {
    list.innerHTML = '<div class="contact-empty">no nexuses yet<br><span>create one or join with a code</span></div>'
    return
  }

  list.innerHTML = S.nexuses.map(n => {
    const msgs = S.conversations[n.id] ?? []
    const last = msgs[msgs.length - 1]
    const preview = last
      ? `<span class="preview-sender">${escHtml(last.senderUsername ?? last.senderKey.slice(0, 8))}:</span> ${escHtml(last.text.slice(0, 50))}`
      : ''
    const active = S.activeNexusId === n.id ? 'active' : ''
    return `
      <div class="contact-item ${active}" data-nexus-id="${escAttr(n.id)}">
        <div class="contact-name">${escHtml(n.name.toUpperCase())}</div>
        <div class="contact-key-short">${n.members?.length ?? '?'} members</div>
        ${preview ? `<div class="contact-preview">${preview}</div>` : ''}
      </div>`
  }).join('')
}

// ─── Chat view ──────────────────────────────────────────────────────────────

function activeNexus(): Nexus | undefined {
  return S.nexuses.find(n => n.id === S.activeNexusId)
}

function openChat(nexus: Nexus): void {
  S.activeNexusId = nexus.id
  S.conversations[nexus.id] ??= storage.loadMessages(nexus.id)

  $('chat-empty').classList.add('hidden')
  $('chat-view').classList.remove('hidden')
  $('chat-header-name').textContent = nexus.name.toUpperCase()
  $('chat-header-key').textContent = `${nexus.members?.length ?? '?'} members`

  if (S.isMobile) {
    $('sidebar').classList.add('mobile-hidden')
    $('chat-area').classList.add('mobile-active')
  }

  renderNexusList()
  renderMessages(true)
  $('message-input').focus()
}

function closeChat(): void {
  S.activeNexusId = null
  $('chat-empty').classList.remove('hidden')
  $('chat-view').classList.add('hidden')
  if (S.isMobile) {
    $('sidebar').classList.remove('mobile-hidden')
    $('chat-area').classList.remove('mobile-active')
  }
  renderNexusList()
}

function renderMessages(scrollToBottom = false): void {
  if (!S.activeNexusId) return
  const msgs = S.conversations[S.activeNexusId] ?? []
  const hasMore = S.hasMoreHistory[S.activeNexusId] !== false

  const listOuter = $('message-list')
  const list = $('message-list-inner')
  const prevScrollHeight = listOuter.scrollHeight
  const prevScrollTop = listOuter.scrollTop
  const wasAtBottom = prevScrollHeight - prevScrollTop - listOuter.clientHeight < 40

  list.innerHTML = `
    ${hasMore ? `<div class="load-earlier"><button id="btn-load-earlier">load earlier messages</button></div>` : ''}
    ${msgs.map(m => `
      <div class="msg-row ${m.isOutgoing ? 'outgoing' : 'incoming'}">
        <div class="msg-bubble">
          ${!m.isOutgoing ? `<div class="msg-sender">${escHtml(m.senderUsername ?? m.senderKey.slice(0, 8))}</div>` : ''}
          <div class="msg-text">${escHtml(m.text)}</div>
          <div class="msg-time">${formatTime(m.createdAt)}</div>
        </div>
      </div>`).join('')}
  `

  document.getElementById('btn-load-earlier')?.addEventListener('click', loadOlderMessages)

  if (scrollToBottom || wasAtBottom) {
    listOuter.scrollTop = listOuter.scrollHeight
  } else {
    listOuter.scrollTop = prevScrollTop + (listOuter.scrollHeight - prevScrollHeight)
  }
}

// ─── Send message ───────────────────────────────────────────────────────────

async function sendMessage(): Promise<void> {
  if (!S.signingPriv || !S.agreementPriv || !S.activeNexusId) return
  const nexus = activeNexus()
  if (!nexus || !nexus.members?.length) return

  const input = $<HTMLTextAreaElement>('message-input')
  const text = input.value.trim()
  if (!text) return

  input.value = ''
  autoResizeInput()
  updateSendButton()
  setChatError('')

  try {
    // Encrypt for each member (including self).
    const envelopes: api.Envelope[] = []
    for (const member of nexus.members) {
      if (!member.encryptionKey) continue
      const { ephemeralKey, ciphertext } = crypto.encrypt(S.agreementPriv, member.encryptionKey, text)
      envelopes.push({ recipient_key: member.identityKey, ephemeral_key: ephemeralKey, ciphertext })
    }

    const resp = await api.sendNexusMessage(S.signingPriv, nexus.id, envelopes)

    addToConversation(nexus.id, {
      id: resp.id,
      nexusId: nexus.id,
      senderKey: S.myIdentKey,
      senderUsername: S.username ?? undefined,
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

// ─── Older messages ─────────────────────────────────────────────────────────

async function loadOlderMessages(): Promise<void> {
  if (!S.signingPriv || !S.activeNexusId) return
  const beforeId = storage.oldestMessageId(S.activeNexusId)
  const limit = 50
  try {
    const raw = await api.getNexusHistory(S.signingPriv, S.activeNexusId, { limit, beforeId })
    processHistoryBatch(S.activeNexusId, [...raw].reverse(), true)
    if (raw.length < limit) S.hasMoreHistory[S.activeNexusId] = false
    renderMessages()
  } catch (e) {
    console.error('[history] load older failed:', e)
  }
}

// ─── Sync ───────────────────────────────────────────────────────────────────

function scheduleSync(): void {
  doSync()
  setInterval(doSync, 60_000)
}

async function doSync(): Promise<void> {
  await syncNexuses()
  await syncHistory()
  await fetchPending()
}

async function syncNexuses(): Promise<void> {
  if (!S.signingPriv) return
  try {
    const serverNexuses = await api.getNexuses(S.signingPriv)
    for (const sn of serverNexuses) {
      const existing = S.nexuses.find(n => n.id === sn.id)
      if (existing) {
        existing.name = sn.name
        existing.role = sn.role
      } else {
        S.nexuses.push({ id: sn.id, name: sn.name, creatorKey: sn.creator_key, role: sn.role, members: [] })
        S.conversations[sn.id] ??= storage.loadMessages(sn.id)
      }
    }
    // Remove nexuses we're no longer a member of.
    const serverIds = new Set(serverNexuses.map(n => n.id))
    S.nexuses = S.nexuses.filter(n => serverIds.has(n.id))

    // Refresh members for each nexus.
    for (const nexus of S.nexuses) {
      try {
        const detail = await api.getNexus(S.signingPriv, nexus.id)
        nexus.members = detail.members.map(m => ({
          identityKey: m.identity_key,
          username: m.username,
          encryptionKey: m.encryption_key,
          role: m.role,
        }))
      } catch { /* skip */ }
    }

    storage.saveNexuses(S.nexuses)
    renderNexusList()
  } catch (e) {
    console.error('[sync] nexuses failed:', e)
  }
}

async function syncHistory(): Promise<void> {
  if (!S.signingPriv) return
  for (const nexus of S.nexuses) {
    const since = storage.newestMessageDate(nexus.id)
    try {
      const msgs = await api.getNexusHistory(S.signingPriv, nexus.id, { limit: 200, since })
      processHistoryBatch(nexus.id, msgs)
    } catch (e) {
      console.error('[sync] history failed for', nexus.id, e)
    }
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

function processHistoryBatch(nexusId: string, msgs: ServerMessage[], prepend = false): void {
  if (!S.agreementPriv) return
  for (const m of msgs) {
    const isOutgoing = m.sender_key === S.myIdentKey
    let text: string
    try {
      text = crypto.decrypt(S.agreementPriv, m.ephemeral_key, m.ciphertext)
    } catch { continue }

    // Look up sender username from nexus members.
    const nexus = S.nexuses.find(n => n.id === nexusId)
    const senderMember = nexus?.members?.find(mb => mb.identityKey === m.sender_key)

    const stored: StoredMessage = {
      id: m.id,
      nexusId: nexusId,
      senderKey: m.sender_key,
      senderUsername: senderMember?.username,
      text,
      createdAt: m.created_at,
      isOutgoing,
    }

    if (prepend) {
      storage.prependMessages(nexusId, [stored])
      const arr = S.conversations[nexusId] ?? []
      if (!arr.some(x => x.id === stored.id)) {
        S.conversations[nexusId] = [stored, ...arr]
      }
    } else {
      addToConversation(nexusId, stored)
    }
  }
  renderNexusList()
  if (S.activeNexusId) renderMessages()
}

// ─── WebSocket ──────────────────────────────────────────────────────────────

function connectWS(): void {
  if (!S.signingPriv) return
  if (S.ws) { S.ws.onclose = null; S.ws.close(); S.ws = null }
  if (S.wsReconnectTimer) { clearTimeout(S.wsReconnectTimer); S.wsReconnectTimer = null }

  S.ws = api.openWebSocket(S.signingPriv, {
    onMessage: frame => handleIncoming(frame),
    onMemberEvent: event => handleMemberEvent(event),
    onOpen: () => { S.wsStatus = 'connected'; renderWsStatus() },
    onClose: () => {
      S.wsStatus = 'disconnected'
      renderWsStatus()
      S.ws = null
      S.wsReconnectTimer = setTimeout(connectWS, 5000)
    },
  })
}

function handleIncoming(frame: ServerMessage & { sender_username?: string }): void {
  if (!S.agreementPriv) return

  const nexusId = frame.nexus_id
  const isOutgoing = frame.sender_key === S.myIdentKey

  let text: string
  try {
    text = crypto.decrypt(S.agreementPriv, frame.ephemeral_key, frame.ciphertext)
  } catch (e) {
    console.error('[ws] decrypt failed', e)
    return
  }

  addToConversation(nexusId, {
    id: frame.id,
    nexusId,
    senderKey: frame.sender_key,
    senderUsername: frame.sender_username,
    text,
    createdAt: frame.created_at,
    isOutgoing,
  })

  if (!isOutgoing && S.activeNexusId !== nexusId) {
    showNotification(nexusId, frame.sender_username ?? frame.sender_key.slice(0, 8), text)
  }
}

function handleMemberEvent(event: MemberEvent): void {
  // Refresh the nexus on membership changes.
  if (!S.signingPriv) return
  if (event.type === 'member_kicked' && event.identity_key === S.myIdentKey) {
    // We got kicked — remove the nexus locally.
    S.nexuses = S.nexuses.filter(n => n.id !== event.nexus_id)
    storage.saveNexuses(S.nexuses)
    if (S.activeNexusId === event.nexus_id) closeChat()
    renderNexusList()
    return
  }
  // For other events, refresh members.
  api.getNexus(S.signingPriv, event.nexus_id).then(detail => {
    const nexus = S.nexuses.find(n => n.id === event.nexus_id)
    if (nexus) {
      nexus.members = detail.members.map(m => ({
        identityKey: m.identity_key,
        username: m.username,
        encryptionKey: m.encryption_key,
        role: m.role,
      }))
      storage.saveNexuses(S.nexuses)
      if (S.activeNexusId === event.nexus_id) {
        $('chat-header-key').textContent = `${nexus.members.length} members`
      }
    }
  }).catch(() => {})
}

function showNotification(nexusId: string, senderName: string, text: string): void {
  if (Notification.permission !== 'granted') return
  const nexus = S.nexuses.find(n => n.id === nexusId)
  const title = nexus ? nexus.name : 'Nexus Zero'
  const n = new Notification(title, { body: `${senderName}: ${text}`, tag: nexusId })
  n.onclick = () => {
    window.focus()
    if (nexus) openChat(nexus)
    n.close()
  }
}

// ─── Helpers ────────────────────────────────────────────────────────────────

function addToConversation(nexusId: string, msg: StoredMessage): void {
  const arr = S.conversations[nexusId] ??= []
  if (arr.some(m => m.id === msg.id)) return
  arr.push(msg)
  storage.appendMessage(nexusId, msg)
  if (S.activeNexusId === nexusId) renderMessages()
  renderNexusList()
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

// ─── Modals ─────────────────────────────────────────────────────────────────

function openModal(id: AppState['modal']): void {
  S.modal = id
  $('modal-overlay').classList.remove('hidden')
  $('modal-create-nexus').classList.toggle('hidden', id !== 'create-nexus')
  $('modal-join-nexus').classList.toggle('hidden', id !== 'join-nexus')
  $('modal-nexus-settings').classList.toggle('hidden', id !== 'nexus-settings')
  $('modal-settings').classList.toggle('hidden', id !== 'settings')
  if (id === 'settings') populateSettings()
  if (id === 'nexus-settings') populateNexusSettings()
}

function closeModal(): void {
  S.modal = null
  $('modal-overlay').classList.add('hidden')
}

function populateSettings(): void {
  $<HTMLInputElement>('settings-server-url').value = storage.getServerUrl()
  $('settings-identity-key').textContent = S.myIdentKey
  $('settings-username').textContent = S.username ?? '(not set)'
  if (S.signingPriv && S.agreementPriv) {
    $('settings-backup-code').textContent = crypto.makeBackupCode(S.signingPriv, S.agreementPriv)
  }
}

async function handleCreateNexus(): Promise<void> {
  if (!S.signingPriv) return
  const name = $<HTMLInputElement>('create-nexus-name').value.trim()
  $('create-nexus-error').textContent = ''
  if (!name) { $('create-nexus-error').textContent = 'Name is required'; return }

  try {
    const nexus = await api.createNexus(S.signingPriv, name)
    // Fetch full detail with members.
    const detail = await api.getNexus(S.signingPriv, nexus.id)
    S.nexuses.push({
      id: detail.id,
      name: detail.name,
      creatorKey: detail.creator_key,
      role: detail.role,
      members: detail.members.map(m => ({
        identityKey: m.identity_key,
        username: m.username,
        encryptionKey: m.encryption_key,
        role: m.role,
      })),
    })
    S.conversations[nexus.id] = []
    storage.saveNexuses(S.nexuses)
    $<HTMLInputElement>('create-nexus-name').value = ''
    closeModal()
    renderNexusList()
  } catch (e) {
    $('create-nexus-error').textContent = (e as Error).message
  }
}

async function handleJoinNexus(): Promise<void> {
  if (!S.signingPriv) return
  const code = $<HTMLInputElement>('join-nexus-code').value.trim().toUpperCase()
  $('join-nexus-error').textContent = ''
  if (!code) { $('join-nexus-error').textContent = 'Invite code is required'; return }

  try {
    const result = await api.joinNexus(S.signingPriv, code)
    // Fetch full detail.
    const detail = await api.getNexus(S.signingPriv, result.nexus_id)
    if (!S.nexuses.find(n => n.id === result.nexus_id)) {
      S.nexuses.push({
        id: detail.id,
        name: detail.name,
        creatorKey: detail.creator_key,
        role: 'member',
        members: detail.members.map(m => ({
          identityKey: m.identity_key,
          username: m.username,
          encryptionKey: m.encryption_key,
          role: m.role,
        })),
      })
      S.conversations[result.nexus_id] = []
    }
    storage.saveNexuses(S.nexuses)
    $<HTMLInputElement>('join-nexus-code').value = ''
    closeModal()
    renderNexusList()
  } catch (e) {
    $('join-nexus-error').textContent = (e as Error).message
  }
}

function populateNexusSettings(): void {
  const nexus = activeNexus()
  if (!nexus) return

  $('nexus-settings-name').textContent = nexus.name
  const isAdmin = nexus.role === 'admin'

  // Members list
  const memberList = $('nexus-settings-members')
  memberList.innerHTML = (nexus.members ?? []).map(m => {
    const isSelf = m.identityKey === S.myIdentKey
    const kickBtn = isAdmin && !isSelf
      ? `<button class="btn-kick" data-key="${escAttr(m.identityKey)}">kick</button>`
      : ''
    return `<div class="member-row">
      <span class="member-name">${escHtml(m.username ?? m.identityKey.slice(0, 8))}${m.role === 'admin' ? ' (admin)' : ''}${isSelf ? ' (you)' : ''}</span>
      ${kickBtn}
    </div>`
  }).join('')

  // Kick handlers
  memberList.querySelectorAll('.btn-kick').forEach(btn => {
    btn.addEventListener('click', async () => {
      if (!S.signingPriv || !nexus) return
      const key = (btn as HTMLElement).dataset['key']!
      try {
        await api.kickMember(S.signingPriv, nexus.id, key)
        nexus.members = nexus.members.filter(m => m.identityKey !== key)
        storage.saveNexuses(S.nexuses)
        populateNexusSettings()
      } catch (e) {
        console.error('kick failed:', e)
      }
    })
  })

  // Invite section (admin only)
  $('nexus-invite-section').classList.toggle('hidden', !isAdmin)
  $('nexus-invite-code').textContent = ''
}

async function handleGenerateInvite(): Promise<void> {
  if (!S.signingPriv || !S.activeNexusId) return
  try {
    const invite = await api.createInvite(S.signingPriv, S.activeNexusId)
    $('nexus-invite-code').textContent = invite.code
  } catch (e) {
    console.error('create invite failed:', e)
  }
}

function handleSaveSettings(): void {
  const url = $<HTMLInputElement>('settings-server-url').value.trim()
  if (url) storage.setServerUrl(url)
  closeModal()
  connectWS()
}

// ─── Event bindings ─────────────────────────────────────────────────────────

function bindEvents(): void {
  // Onboarding
  $('btn-generate').addEventListener('click', () => { void handleGenerate() })
  $('btn-restore').addEventListener('click', () => { setObError(''); showObPanel('ob-restore') })
  $('btn-backup-done').addEventListener('click', handleBackupDone)
  $('btn-restore-submit').addEventListener('click', () => { void handleRestore() })
  $<HTMLInputElement>('restore-input').addEventListener('keydown', e => { if (e.key === 'Enter') void handleRestore() })

  // Username
  $('btn-set-username').addEventListener('click', () => { void handleSetUsername() })
  $<HTMLInputElement>('username-input').addEventListener('keydown', e => { if (e.key === 'Enter') void handleSetUsername() })

  // Nexus list clicks
  $('nexus-list').addEventListener('click', e => {
    const item = (e.target as Element).closest<HTMLElement>('.contact-item')
    if (!item?.dataset['nexusId']) return
    const nexus = S.nexuses.find(n => n.id === item.dataset['nexusId'])
    if (nexus) openChat(nexus)
  })

  // Toolbar buttons
  $('btn-create-nexus').addEventListener('click', () => openModal('create-nexus'))
  $('btn-join-nexus').addEventListener('click', () => openModal('join-nexus'))
  $('btn-settings').addEventListener('click', () => openModal('settings'))
  $('btn-back').addEventListener('click', closeChat)
  $('btn-nexus-settings').addEventListener('click', () => openModal('nexus-settings'))

  // Chat input
  $<HTMLTextAreaElement>('message-input').addEventListener('input', () => { autoResizeInput(); updateSendButton() })
  $<HTMLTextAreaElement>('message-input').addEventListener('keydown', e => {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); void sendMessage() }
  })
  $('btn-send').addEventListener('click', () => { void sendMessage() })

  // Modals
  $('modal-overlay').addEventListener('click', e => { if (e.target === $('modal-overlay')) closeModal() })
  $('btn-create-nexus-submit').addEventListener('click', () => { void handleCreateNexus() })
  $('btn-create-nexus-cancel').addEventListener('click', closeModal)
  $<HTMLInputElement>('create-nexus-name').addEventListener('keydown', e => { if (e.key === 'Enter') void handleCreateNexus() })
  $('btn-join-nexus-submit').addEventListener('click', () => { void handleJoinNexus() })
  $('btn-join-nexus-cancel').addEventListener('click', closeModal)
  $<HTMLInputElement>('join-nexus-code').addEventListener('keydown', e => { if (e.key === 'Enter') void handleJoinNexus() })
  $('btn-generate-invite').addEventListener('click', () => { void handleGenerateInvite() })
  $('btn-nexus-settings-close').addEventListener('click', closeModal)
  $('btn-settings-close').addEventListener('click', closeModal)
  $('btn-settings-save').addEventListener('click', handleSaveSettings)

  document.addEventListener('click', () => {
    if (Notification.permission === 'default') void Notification.requestPermission()
  }, { once: true })

  window.addEventListener('resize', () => { S.isMobile = window.innerWidth <= 640 })
}
