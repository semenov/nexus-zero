import type { Keys } from './crypto.js'

export interface NexusMember {
  identityKey: string
  username?: string
  encryptionKey: string
  role: string
}

export interface Nexus {
  id: string
  name: string
  creatorKey: string
  role: string
  members: NexusMember[]
}

export interface StoredMessage {
  id: string
  nexusId: string
  senderKey: string
  senderUsername?: string
  text: string
  createdAt: string
  isOutgoing: boolean
}

const P = 'nxz_'

function get<T>(key: string): T | null {
  try { return JSON.parse(localStorage.getItem(P + key) ?? 'null') as T } catch { return null }
}
function set(key: string, val: unknown): void { localStorage.setItem(P + key, JSON.stringify(val)) }

// ── Keys ────────────────────────────────────────────────────────────────────

export function loadKeys(): Keys | null {
  const d = get<{ sp: string; ap: string }>('keys')
  if (!d) return null
  const decode = (s: string) => new Uint8Array(atob(s).split('').map(c => c.charCodeAt(0)))
  return { signingPriv: decode(d.sp), agreementPriv: decode(d.ap) }
}

export function saveKeys(signingPriv: Uint8Array, agreementPriv: Uint8Array): void {
  const encode = (b: Uint8Array) => btoa(String.fromCharCode(...b))
  set('keys', { sp: encode(signingPriv), ap: encode(agreementPriv) })
}

// ── Server URL ──────────────────────────────────────────────────────────────

export function getServerUrl(): string { return get<string>('server_url') ?? 'https://nexus.semenov.ai' }
export function setServerUrl(url: string): void { set('server_url', url) }

// ── Username ────────────────────────────────────────────────────────────────

export function loadUsername(): string | null { return get<string>('username') }
export function saveUsername(username: string): void { set('username', username) }

// ── Nexuses ─────────────────────────────────────────────────────────────────

export function loadNexuses(): Nexus[] { return get<Nexus[]>('nexuses') ?? [] }
export function saveNexuses(nexuses: Nexus[]): void { set('nexuses', nexuses) }

// ── Messages ────────────────────────────────────────────────────────────────

function msgsKey(nexusId: string): string {
  return 'msgs_' + nexusId.replace(/[^A-Za-z0-9-]/g, '_')
}

export function loadMessages(nexusId: string): StoredMessage[] {
  return get<StoredMessage[]>(msgsKey(nexusId)) ?? []
}

export function saveMessages(nexusId: string, messages: StoredMessage[]): void {
  set(msgsKey(nexusId), messages)
}

export function appendMessage(nexusId: string, message: StoredMessage): boolean {
  const msgs = loadMessages(nexusId)
  if (msgs.some(m => m.id === message.id)) return false
  msgs.push(message)
  saveMessages(nexusId, msgs)
  return true
}

export function prependMessages(nexusId: string, newMsgs: StoredMessage[]): void {
  const existing = loadMessages(nexusId)
  const existingIds = new Set(existing.map(m => m.id))
  const toAdd = newMsgs.filter(m => !existingIds.has(m.id))
  if (toAdd.length === 0) return
  saveMessages(nexusId, [...toAdd, ...existing])
}

export function newestMessageDate(nexusId: string): Date | null {
  const msgs = loadMessages(nexusId)
  if (!msgs.length) return null
  return msgs.reduce((a, b) => new Date(a.createdAt) > new Date(b.createdAt) ? a : b).createdAt
    ? new Date(msgs.reduce((a, b) => new Date(a.createdAt) > new Date(b.createdAt) ? a : b).createdAt)
    : null
}

export function oldestMessageId(nexusId: string): string | null {
  const msgs = loadMessages(nexusId)
  if (!msgs.length) return null
  return msgs.reduce((a, b) => new Date(a.createdAt) <= new Date(b.createdAt) ? a : b).id
}
