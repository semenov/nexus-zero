import { makeAuthHeader, makeAuthToken } from './crypto.js'
import { getServerUrl } from './storage.js'

// ── Server response shapes ────────────────────────────────────────────────────

export interface ServerUser {
  identity_key: string
  encryption_key: string
  created_at: string
}

export interface ServerContact {
  contact_key: string
  nickname: string
  encryption_key?: string
  updated_at: string
}

export interface ServerMessage {
  id: string
  sender_key: string
  recipient_key?: string
  ephemeral_key: string
  ciphertext: string
  sender_ephemeral_key?: string
  sender_ciphertext?: string
  created_at: string
}

export interface SendMessageResponse {
  id: string
  created_at: string
}

export interface SendMessageParams {
  recipientKey: string
  ephemeralKey: string
  ciphertext: string
  senderEphemeralKey: string
  senderCiphertext: string
}

export interface GetHistoryParams {
  limit?: number
  since?: Date | null
  beforeId?: string | null
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

async function req(path: string, opts: RequestInit = {}, signingPriv?: Uint8Array): Promise<Response> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json', ...(opts.headers as Record<string, string> ?? {}) }
  if (signingPriv) headers['Authorization'] = makeAuthHeader(signingPriv)
  return fetch(getServerUrl() + path, { ...opts, headers })
}

async function checkOk(resp: Response, expected = 200): Promise<void> {
  if (resp.status === expected) return
  let msg = `HTTP ${resp.status}`
  try { const b = await resp.json() as { error?: string }; if (b.error) msg = b.error } catch {}
  throw new Error(msg)
}

// ── Endpoints ─────────────────────────────────────────────────────────────────

export async function registerUser(signingPriv: Uint8Array, identKeyB64: string, encKeyB64: string): Promise<void> {
  const resp = await req('/v1/users', {
    method: 'POST',
    body: JSON.stringify({ identity_key: identKeyB64, encryption_key: encKeyB64 }),
  })
  if (resp.status !== 201 && resp.status !== 409) await checkOk(resp, 201)
}

export async function getUser(identityKey: string): Promise<ServerUser | null> {
  const resp = await req(`/v1/users/${encodeURIComponent(identityKey)}`)
  if (resp.status === 404) return null
  await checkOk(resp)
  return resp.json() as Promise<ServerUser>
}

export async function sendMessage(signingPriv: Uint8Array, params: SendMessageParams): Promise<SendMessageResponse> {
  const resp = await req('/v1/messages', {
    method: 'POST',
    body: JSON.stringify({
      recipient_key: params.recipientKey,
      ephemeral_key: params.ephemeralKey,
      ciphertext: params.ciphertext,
      sender_ephemeral_key: params.senderEphemeralKey,
      sender_ciphertext: params.senderCiphertext,
    }),
  }, signingPriv)
  await checkOk(resp, 201)
  return resp.json() as Promise<SendMessageResponse>
}

export async function getHistory(signingPriv: Uint8Array, { limit = 100, since = null, beforeId = null }: GetHistoryParams = {}): Promise<ServerMessage[]> {
  const p = new URLSearchParams({ limit: String(limit) })
  if (since) p.set('since', since.toISOString())
  if (beforeId) p.set('before_id', beforeId)
  const resp = await req(`/v1/messages/history?${p}`, {}, signingPriv)
  await checkOk(resp)
  return resp.json() as Promise<ServerMessage[]>
}

export async function getPendingMessages(signingPriv: Uint8Array): Promise<ServerMessage[]> {
  const resp = await req('/v1/messages/pending', {}, signingPriv)
  await checkOk(resp)
  return resp.json() as Promise<ServerMessage[]>
}

export async function getContacts(signingPriv: Uint8Array): Promise<ServerContact[]> {
  const resp = await req('/v1/contacts', {}, signingPriv)
  await checkOk(resp)
  return resp.json() as Promise<ServerContact[]>
}

export async function upsertContact(signingPriv: Uint8Array, contactKey: string, nickname: string): Promise<void> {
  const resp = await req(`/v1/contacts/${encodeURIComponent(contactKey)}`, {
    method: 'PUT',
    body: JSON.stringify({ nickname }),
  }, signingPriv)
  await checkOk(resp, 204)
}

export async function deleteContact(signingPriv: Uint8Array, contactKey: string): Promise<void> {
  const resp = await req(`/v1/contacts/${encodeURIComponent(contactKey)}`, { method: 'DELETE' }, signingPriv)
  await checkOk(resp, 204)
}

// ── WebSocket ─────────────────────────────────────────────────────────────────

export interface WebSocketCallbacks {
  onMessage: (frame: ServerMessage) => void
  onOpen?: () => void
  onClose?: () => void
}

export function openWebSocket(signingPriv: Uint8Array, callbacks: WebSocketCallbacks): WebSocket {
  const base = getServerUrl().replace(/^http/, 'ws')
  const token = encodeURIComponent(makeAuthToken(signingPriv))
  const ws = new WebSocket(`${base}/v1/ws?auth=${token}`)

  ws.addEventListener('open', () => callbacks.onOpen?.())
  ws.addEventListener('close', () => callbacks.onClose?.())
  ws.addEventListener('error', () => callbacks.onClose?.())
  ws.addEventListener('message', ({ data }) => {
    try {
      const frame = JSON.parse(data as string) as ServerMessage & { type?: string }
      if (frame.type !== 'message') return
      callbacks.onMessage(frame)
      ws.send(JSON.stringify({ type: 'ack', id: frame.id }))
    } catch (e) {
      console.error('[ws] parse error', e)
    }
  })

  return ws
}
