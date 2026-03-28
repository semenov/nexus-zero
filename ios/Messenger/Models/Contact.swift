import Foundation

/// A nexus (group chat) that the user is a member of.
struct Nexus: Identifiable, Codable, Equatable, Hashable {
    let id: String
    let name: String
    let creatorKey: String
    let role: String
    var members: [NexusMember]
}

/// A member of a nexus.
struct NexusMember: Codable, Equatable, Hashable {
    let identityKey: String
    let username: String?
    let encryptionKey: String
    let role: String
}
