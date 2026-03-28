import { makeAuthHeader, makeAuthToken } from './crypto.js'
import { getServerUrl } from './storage.js'

// ── Server response shapes ──────────────────────────────────────────────────

export interface ServerUser {
  identity_key: string
  encryption_key: string
  username?: string
  created_at: string
}

export interface ServerNexus {
  id: string
  name: string
  creator_key: string
  created_at: string
  role: string
}

export interface ServerNexusDetail extends ServerNexus {
  members: ServerNexusMember[]
}

export interface ServerNexusMember {
  identity_key: string
  username?: string
  encryption_key: string
  role: string
  joined_at: string
}

export interface ServerInviteCode {
  id: string
  nexus_id: string
  code: string
  created_by: string
  max_uses?: number
  use_count: number
  revoked: boolean
  created_at: string
  expires_at?: string
}

export interface ServerMessage {
  id: string
  nexus_id: string
  sender_key: string
  recipient_key: string
  ephemeral_key: string
  ciphertext: string
  created_at: string
}

export interface SendMessageResponse {
  id: string
  created_at: string
}

export interface JoinResponse {
  nexus_id: string
  name: string
}

export interface MemberEvent {
  type: 'member_joined' | 'member_left' | 'member_kicked'
  nexus_id: string
  identity_key: string
  username?: string
}

// ── HTTP helpers ────────────────────────────────────────────────────────────

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

// ── Users ───────────────────────────────────────────────────────────────────

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

export async function setUsername(signingPriv: Uint8Array, username: string): Promise<void> {
  const resp = await req('/v1/users/me/username', {
    method: 'PUT',
    body: JSON.stringify({ username }),
  }, signingPriv)
  await checkOk(resp, 204)
}

// ── Nexuses ─────────────────────────────────────────────────────────────────

export async function createNexus(signingPriv: Uint8Array, name: string): Promise<ServerNexus> {
  const resp = await req('/v1/nexuses', { method: 'POST', body: JSON.stringify({ name }) }, signingPriv)
  await checkOk(resp, 201)
  return resp.json() as Promise<ServerNexus>
}

export async function getNexuses(signingPriv: Uint8Array): Promise<ServerNexus[]> {
  const resp = await req('/v1/nexuses', {}, signingPriv)
  await checkOk(resp)
  return resp.json() as Promise<ServerNexus[]>
}

export async function getNexus(signingPriv: Uint8Array, nexusId: string): Promise<ServerNexusDetail> {
  const resp = await req(`/v1/nexuses/${nexusId}`, {}, signingPriv)
  await checkOk(resp)
  return resp.json() as Promise<ServerNexusDetail>
}

export async function updateNexus(signingPriv: Uint8Array, nexusId: string, name: string): Promise<void> {
  const resp = await req(`/v1/nexuses/${nexusId}`, { method: 'PUT', body: JSON.stringify({ name }) }, signingPriv)
  await checkOk(resp, 204)
}

export async function deleteNexus(signingPriv: Uint8Array, nexusId: string): Promise<void> {
  const resp = await req(`/v1/nexuses/${nexusId}`, { method: 'DELETE' }, signingPriv)
  await checkOk(resp, 204)
}

// ── Members ─────────────────────────────────────────────────────────────────

export async function getMembers(signingPriv: Uint8Array, nexusId: string): Promise<ServerNexusMember[]> {
  const resp = await req(`/v1/nexuses/${nexusId}/members`, {}, signingPriv)
  await checkOk(resp)
  return resp.json() as Promise<ServerNexusMember[]>
}

export async function kickMember(signingPriv: Uint8Array, nexusId: string, identityKey: string): Promise<void> {
  const resp = await req(`/v1/nexuses/${nexusId}/members/${encodeURIComponent(identityKey)}`, { method: 'DELETE' }, signingPriv)
  await checkOk(resp, 204)
}

export async function leaveNexus(signingPriv: Uint8Array, nexusId: string): Promise<void> {
  const resp = await req(`/v1/nexuses/${nexusId}/leave`, { method: 'POST' }, signingPriv)
  await checkOk(resp, 204)
}

// ── Invites ─────────────────────────────────────────────────────────────────

export async function createInvite(signingPriv: Uint8Array, nexusId: string, opts?: { maxUses?: number; expiresInHours?: number }): Promise<ServerInviteCode> {
  const body: Record<string, unknown> = {}
  if (opts?.maxUses) body.max_uses = opts.maxUses
  if (opts?.expiresInHours) body.expires_in_hours = opts.expiresInHours
  const resp = await req(`/v1/nexuses/${nexusId}/invites`, { method: 'POST', body: JSON.stringify(body) }, signingPriv)
  await checkOk(resp, 201)
  return resp.json() as Promise<ServerInviteCode>
}

export async function getInvites(signingPriv: Uint8Array, nexusId: string): Promise<ServerInviteCode[]> {
  const resp = await req(`/v1/nexuses/${nexusId}/invites`, {}, signingPriv)
  await checkOk(resp)
  return resp.json() as Promise<ServerInviteCode[]>
}

export async function revokeInvite(signingPriv: Uint8Array, nexusId: string, inviteId: string): Promise<void> {
  const resp = await req(`/v1/nexuses/${nexusId}/invites/${inviteId}`, { method: 'DELETE' }, signingPriv)
  await checkOk(resp, 204)
}

// ── Join ────────────────────────────────────────────────────────────────────

export async function joinNexus(signingPriv: Uint8Array, code: string): Promise<JoinResponse> {
  const resp = await req('/v1/join', { method: 'POST', body: JSON.stringify({ code }) }, signingPriv)
  await checkOk(resp)
  return resp.json() as Promise<JoinResponse>
}

// ── Messages ────────────────────────────────────────────────────────────────

export interface Envelope {
  recipient_key: string
  ephemeral_key: string
  ciphertext: string
}

export async function sendNexusMessage(signingPriv: Uint8Array, nexusId: string, envelopes: Envelope[]): Promise<SendMessageResponse> {
  const resp = await req(`/v1/nexuses/${nexusId}/messages`, {
    method: 'POST',
    body: JSON.stringify({ envelopes }),
  }, signingPriv)
  await checkOk(resp, 201)
  return resp.json() as Promise<SendMessageResponse>
}

export interface GetHistoryParams {
  limit?: number
  since?: Date | null
  beforeId?: string | null
}

export async function getNexusHistory(signingPriv: Uint8Array, nexusId: string, { limit = 100, since = null, beforeId = null }: GetHistoryParams = {}): Promise<ServerMessage[]> {
  const p = new URLSearchParams({ limit: String(limit) })
  if (since) p.set('since', since.toISOString())
  if (beforeId) p.set('before_id', beforeId)
  const resp = await req(`/v1/nexuses/${nexusId}/messages?${p}`, {}, signingPriv)
  await checkOk(resp)
  return resp.json() as Promise<ServerMessage[]>
}

export async function getPendingMessages(signingPriv: Uint8Array): Promise<ServerMessage[]> {
  const resp = await req('/v1/messages/pending', {}, signingPriv)
  await checkOk(resp)
  return resp.json() as Promise<ServerMessage[]>
}

// ── Device Token ────────────────────────────────────────────────────────────

export async function registerDeviceToken(signingPriv: Uint8Array, token: string): Promise<void> {
  const resp = await req('/v1/device-token', { method: 'PUT', body: JSON.stringify({ token }) }, signingPriv)
  await checkOk(resp, 204)
}

// ── WebSocket ───────────────────────────────────────────────────────────────

export interface WebSocketCallbacks {
  onMessage: (frame: ServerMessage & { sender_username?: string }) => void
  onMemberEvent?: (event: MemberEvent) => void
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
      const frame = JSON.parse(data as string) as { type: string; [k: string]: unknown }
      if (frame.type === 'message') {
        callbacks.onMessage(frame as unknown as ServerMessage & { sender_username?: string })
        ws.send(JSON.stringify({ type: 'ack', id: (frame as { id: string }).id }))
      } else if (frame.type === 'member_joined' || frame.type === 'member_left' || frame.type === 'member_kicked') {
        callbacks.onMemberEvent?.(frame as unknown as MemberEvent)
      }
    } catch (e) {
      console.error('[ws] parse error', e)
    }
  })

  return ws
}
