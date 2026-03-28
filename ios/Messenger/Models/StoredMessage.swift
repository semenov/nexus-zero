import Foundation

/// A decrypted message persisted locally in a nexus conversation.
struct StoredMessage: Identifiable, Codable {
    /// Server-assigned UUID.
    let id: String
    /// Nexus this message belongs to.
    let nexusId: String
    /// Identity key (Ed25519 public, base64url) of the original sender.
    let senderKey: String
    /// Sender's username at time of message, if known.
    let senderUsername: String?
    /// Plain-text content after decryption.
    let text: String
    /// When the message was created on the server.
    let createdAt: Date
    /// `true` when this device sent the message, `false` when it was received.
    let isOutgoing: Bool
}
