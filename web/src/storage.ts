import type { Keys } from './crypto.js'

export interface Contact {
  identityKey: string
  encryptionKey: string
  nickname: string
}

export interface StoredMessage {
  id: string
  senderKey: string
  text: string
  createdAt: string
  isOutgoing: boolean
}

const P = 'nxz_'

function get<T>(key: string): T | null {
  try { return JSON.parse(localStorage.getItem(P + key) ?? 'null') as T } catch { return null }
}
function set(key: string, val: unknown): void { localStorage.setItem(P + key, JSON.stringify(val)) }
function del(key: string): void { localStorage.removeItem(P + key) }

// ── Keys ──────────────────────────────────────────────────────────────────────

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

export function clearKeys(): void { del('keys') }

// ── Server URL ────────────────────────────────────────────────────────────────

export function getServerUrl(): string { return get<string>('server_url') ?? 'http://localhost:8888' }
export function setServerUrl(url: string): void { set('server_url', url) }

// ── Contacts ──────────────────────────────────────────────────────────────────

export function loadContacts(): Contact[] { return get<Contact[]>('contacts') ?? [] }
export function saveContacts(contacts: Contact[]): void { set('contacts', contacts) }

// ── Messages ──────────────────────────────────────────────────────────────────

function msgsKey(identityKey: string): string {
  return 'msgs_' + identityKey.replace(/[^A-Za-z0-9]/g, '_')
}

export function loadMessages(identityKey: string): StoredMessage[] {
  return get<StoredMessage[]>(msgsKey(identityKey)) ?? []
}

export function saveMessages(identityKey: string, messages: StoredMessage[]): void {
  set(msgsKey(identityKey), messages)
}

export function appendMessage(identityKey: string, message: StoredMessage): boolean {
  const msgs = loadMessages(identityKey)
  if (msgs.some(m => m.id === message.id)) return false
  msgs.push(message)
  saveMessages(identityKey, msgs)
  return true
}

export function prependMessages(identityKey: string, newMsgs: StoredMessage[]): void {
  const existing = loadMessages(identityKey)
  const existingIds = new Set(existing.map(m => m.id))
  const toAdd = newMsgs.filter(m => !existingIds.has(m.id))
  if (toAdd.length === 0) return
  saveMessages(identityKey, [...toAdd, ...existing])
}

// Latest createdAt across all conversations (for incremental history sync)
export function newestMessageDate(): Date | null {
  let newest: Date | null = null
  for (let i = 0; i < localStorage.length; i++) {
    const key = localStorage.key(i)
    if (!key?.startsWith(P + 'msgs_')) continue
    try {
      const msgs = JSON.parse(localStorage.getItem(key) ?? '[]') as StoredMessage[]
      for (const m of msgs) {
        const d = new Date(m.createdAt)
        if (!newest || d > newest) newest = d
      }
    } catch { /* ignore corrupt entries */ }
  }
  return newest
}

// ID of oldest stored message for a contact (for "load earlier" pagination)
export function oldestMessageId(identityKey: string): string | null {
  const msgs = loadMessages(identityKey)
  if (!msgs.length) return null
  return msgs.reduce((a, b) => new Date(a.createdAt) <= new Date(b.createdAt) ? a : b).id
}
