import { ed25519, x25519 } from '@noble/curves/ed25519'
import { hkdf } from '@noble/hashes/hkdf'
import { sha256 } from '@noble/hashes/sha256'
import { chacha20poly1305 } from '@noble/ciphers/chacha'

export interface Keys {
  signingPriv: Uint8Array
  agreementPriv: Uint8Array
}

export interface EncryptResult {
  ephemeralKey: string
  ciphertext: string
}

const enc = new TextEncoder()
const dec = new TextDecoder()

// Base64url without padding — matches Go's base64.RawURLEncoding and iOS Data.base64URLEncodedString()
export function b64uEncode(bytes: Uint8Array): string {
  let binary = ''
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]!)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '')
}

export function b64uDecode(str: string): Uint8Array {
  str = str.replace(/-/g, '+').replace(/_/g, '/')
  const pad = str.length % 4
  if (pad) str += '='.repeat(4 - pad)
  const binary = atob(str)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  return bytes
}

export function generateKeys(): Keys {
  return {
    signingPriv: ed25519.utils.randomPrivateKey(),  // 32-byte seed
    agreementPriv: x25519.utils.randomPrivateKey(), // 32-byte seed
  }
}

export function identityKeyB64(signingPriv: Uint8Array): string {
  return b64uEncode(ed25519.getPublicKey(signingPriv))
}

export function encryptionKeyB64(agreementPriv: Uint8Array): string {
  return b64uEncode(x25519.getPublicKey(agreementPriv))
}

// Auth token: "<pubkey_b64>.<unix_timestamp>.<signature_b64>"
export function makeAuthToken(signingPriv: Uint8Array): string {
  const pubB64 = identityKeyB64(signingPriv)
  const ts = Math.floor(Date.now() / 1000).toString()
  const msg = enc.encode(`${pubB64}.${ts}`)
  const sig = ed25519.sign(msg, signingPriv)
  return `${pubB64}.${ts}.${b64uEncode(sig)}`
}

export function makeAuthHeader(signingPriv: Uint8Array): string {
  return `Ed25519 ${makeAuthToken(signingPriv)}`
}

// HKDF-SHA256 — matches iOS CryptoEngine.deriveKey
function deriveSymmetricKey(sharedSecret: Uint8Array): Uint8Array {
  return hkdf(sha256, sharedSecret, enc.encode('messenger-v1'), enc.encode('message'), 32)
}

// Encrypt plaintext for a recipient's X25519 public key (base64url).
// Ciphertext format: nonce(12) || chacha20poly1305(n+16) — matches iOS ChaChaPoly.SealedBox.combined
export function encrypt(agreementPriv: Uint8Array, recipientEncKeyB64: string, plaintext: string): EncryptResult {
  const ephPriv = x25519.utils.randomPrivateKey()
  const ephPub = x25519.getPublicKey(ephPriv)
  const sharedSecret = x25519.getSharedSecret(ephPriv, b64uDecode(recipientEncKeyB64))
  const key = deriveSymmetricKey(sharedSecret)

  const nonce = crypto.getRandomValues(new Uint8Array(12))
  const encrypted = chacha20poly1305(key, nonce).encrypt(enc.encode(plaintext))

  const combined = new Uint8Array(12 + encrypted.length)
  combined.set(nonce, 0)
  combined.set(encrypted, 12)

  return { ephemeralKey: b64uEncode(ephPub), ciphertext: b64uEncode(combined) }
}

// Encrypt for self — sender copy stored on server for history
export function encryptForSelf(agreementPriv: Uint8Array, plaintext: string): EncryptResult {
  return encrypt(agreementPriv, encryptionKeyB64(agreementPriv), plaintext)
}

// Decrypt using local agreement private key
export function decrypt(agreementPriv: Uint8Array, ephKeyB64: string, ciphertextB64: string): string {
  const combined = b64uDecode(ciphertextB64)
  const sharedSecret = x25519.getSharedSecret(agreementPriv, b64uDecode(ephKeyB64))
  const key = deriveSymmetricKey(sharedSecret)
  const plaintext = chacha20poly1305(key, combined.slice(0, 12)).decrypt(combined.slice(12))
  return dec.decode(plaintext)
}

// Backup code: "<signingPriv_b64>.<agreementPriv_b64>" — compatible with iOS format
export function makeBackupCode(signingPriv: Uint8Array, agreementPriv: Uint8Array): string {
  return `${b64uEncode(signingPriv)}.${b64uEncode(agreementPriv)}`
}

export function parseBackupCode(code: string): Keys {
  const parts = code.trim().split('.')
  if (parts.length !== 2) throw new Error('Invalid backup code — expected two base64url parts separated by "."')
  return { signingPriv: b64uDecode(parts[0]!), agreementPriv: b64uDecode(parts[1]!) }
}
