import Foundation

/// A contact whose public keys are known and stored locally.
struct Contact: Identifiable, Codable, Equatable, Hashable {
    /// Stable identifier — equals `identityKey`.
    let id: String
    /// Ed25519 public key used as the contact's identity (base64url, no padding).
    let identityKey: String
    /// X25519 public key used to encrypt messages to this contact (base64url, no padding).
    let encryptionKey: String
    /// Human-readable name chosen by the local user.
    var nickname: String

    init(identityKey: String, encryptionKey: String, nickname: String) {
        self.id = identityKey
        self.identityKey = identityKey
        self.encryptionKey = encryptionKey
        self.nickname = nickname
    }
}
