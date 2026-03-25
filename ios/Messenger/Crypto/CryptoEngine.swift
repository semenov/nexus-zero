import Foundation
import CryptoKit

/// Errors produced by encryption / decryption operations.
enum CryptoEngineError: LocalizedError {
    case missingAgreementKey
    case invalidRecipientKey
    case invalidEphemeralKey
    case invalidCiphertext
    case decryptionFailed(Error)

    var errorDescription: String? {
        switch self {
        case .missingAgreementKey:   return "Local agreement key is not available"
        case .invalidRecipientKey:   return "Recipient encryption key is invalid"
        case .invalidEphemeralKey:   return "Ephemeral public key is invalid"
        case .invalidCiphertext:     return "Ciphertext encoding is invalid"
        case .decryptionFailed(let e): return "Decryption failed: \(e.localizedDescription)"
        }
    }
}

/// Implements the end-to-end encryption scheme.
///
/// **Encryption (send):**
/// 1. Decode recipient's X25519 public key.
/// 2. Generate an ephemeral X25519 keypair.
/// 3. `ECDH(ephemeral_priv, recipient_pub)` → shared secret.
/// 4. `HKDF(shared_secret, salt: "messenger-v1", info: "message")` → 32-byte key.
/// 5. `ChaChaPoly.seal(plaintext, using: symmetricKey)` → combined ciphertext.
///
/// **Decryption (receive):**
/// 1. Decode the ephemeral public key from the message.
/// 2. `ECDH(local_agreement_priv, ephemeral_pub)` → shared secret.
/// 3. Derive key with HKDF (same params).
/// 4. `ChaChaPoly.open(ciphertext, using: symmetricKey)` → plaintext.
final class CryptoEngine {

    let keyManager: KeyManager

    init(keyManager: KeyManager) {
        self.keyManager = keyManager
    }

    // MARK: - Encrypt

    /// Encrypts `text` for the given recipient.
    ///
    /// - Parameters:
    ///   - text: Plain UTF-8 message text.
    ///   - recipientEncryptionKey: Recipient's base64url-encoded X25519 public key.
    /// - Returns: A tuple of base64url-encoded ephemeral public key and ciphertext.
    func encrypt(text: String, recipientEncryptionKey: String) throws -> (ephemeralKey: String, ciphertext: String) {
        guard let recipientKeyData = Data(base64URLEncoded: recipientEncryptionKey) else {
            throw CryptoEngineError.invalidRecipientKey
        }
        let recipientPub = try Curve25519.KeyAgreement.PublicKey(rawRepresentation: recipientKeyData)

        // Ephemeral keypair — created fresh per message.
        let ephemeral = Curve25519.KeyAgreement.PrivateKey()
        let sharedSecret = try ephemeral.sharedSecretFromKeyAgreement(with: recipientPub)

        let symmetricKey = deriveKey(from: sharedSecret)

        let plaintext = Data(text.utf8)
        let sealedBox = try ChaChaPoly.seal(plaintext, using: symmetricKey)

        let ephemeralB64 = ephemeral.publicKey.rawRepresentation.base64URLEncodedString()
        let ciphertextB64 = sealedBox.combined.base64URLEncodedString()
        return (ephemeralB64, ciphertextB64)
    }

    /// Encrypts `text` for the sender themselves (using own agreement public key).
    /// Used to store a readable copy of sent messages on the server.
    func encryptForSelf(text: String) throws -> (ephemeralKey: String, ciphertext: String) {
        guard let localAgreement = keyManager.agreementKey else {
            throw CryptoEngineError.missingAgreementKey
        }
        let selfEncKey = localAgreement.publicKey.rawRepresentation.base64URLEncodedString()
        return try encrypt(text: text, recipientEncryptionKey: selfEncKey)
    }

    // MARK: - Decrypt

    /// Decrypts a message received from another user.
    ///
    /// - Parameters:
    ///   - ephemeralKey: Sender's base64url-encoded ephemeral X25519 public key.
    ///   - ciphertext: Base64url-encoded combined ChaChaPoly ciphertext (nonce + ct + tag).
    /// - Returns: The decrypted plain-text string.
    func decrypt(ephemeralKey: String, ciphertext: String) throws -> String {
        guard let localAgreement = keyManager.agreementKey else {
            throw CryptoEngineError.missingAgreementKey
        }
        guard let ephemeralKeyData = Data(base64URLEncoded: ephemeralKey) else {
            throw CryptoEngineError.invalidEphemeralKey
        }
        guard let ciphertextData = Data(base64URLEncoded: ciphertext) else {
            throw CryptoEngineError.invalidCiphertext
        }

        let ephemeralPub = try Curve25519.KeyAgreement.PublicKey(rawRepresentation: ephemeralKeyData)
        let sharedSecret = try localAgreement.sharedSecretFromKeyAgreement(with: ephemeralPub)

        let symmetricKey = deriveKey(from: sharedSecret)

        do {
            let sealedBox = try ChaChaPoly.SealedBox(combined: ciphertextData)
            let plaintext = try ChaChaPoly.open(sealedBox, using: symmetricKey)
            return String(decoding: plaintext, as: UTF8.self)
        } catch {
            throw CryptoEngineError.decryptionFailed(error)
        }
    }

    // MARK: - HKDF helper

    private func deriveKey(from sharedSecret: SharedSecret) -> SymmetricKey {
        let salt = Data("messenger-v1".utf8)
        let info = Data("message".utf8)
        return sharedSecret.hkdfDerivedSymmetricKey(
            using: SHA256.self,
            salt: salt,
            sharedInfo: info,
            outputByteCount: 32
        )
    }
}
