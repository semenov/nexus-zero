import Foundation

/// Persists contacts and per-conversation message history as JSON files in the
/// app's Documents directory.
final class LocalStore {

    private let fm = FileManager.default

    private var documentsURL: URL {
        fm.urls(for: .documentDirectory, in: .userDomainMask)[0]
    }

    // MARK: - Contacts

    private var contactsURL: URL {
        documentsURL.appendingPathComponent("contacts.json")
    }

    func saveContacts(_ contacts: [Contact]) {
        write(contacts, to: contactsURL)
    }

    func loadContacts() -> [Contact] {
        read([Contact].self, from: contactsURL) ?? []
    }

    // MARK: - Messages

    private func messagesURL(forKey identityKey: String) -> URL {
        // Use a SHA-256 hash of the key as the filename to avoid filesystem
        // issues with special characters in base64url keys.
        let safe = identityKey.replacingOccurrences(of: "/", with: "_")
                              .replacingOccurrences(of: "+", with: "-")
        return documentsURL.appendingPathComponent("msgs_\(safe).json")
    }

    func saveMessages(_ messages: [StoredMessage], forKey identityKey: String) {
        write(messages, to: messagesURL(forKey: identityKey))
    }

    func loadMessages(forKey identityKey: String) -> [StoredMessage] {
        read([StoredMessage].self, from: messagesURL(forKey: identityKey)) ?? []
    }

    func appendMessage(_ message: StoredMessage, forKey identityKey: String) {
        var existing = loadMessages(forKey: identityKey)
        // Avoid duplicates by ID.
        guard !existing.contains(where: { $0.id == message.id }) else { return }
        existing.append(message)
        saveMessages(existing, forKey: identityKey)
    }

    /// Returns the newest `createdAt` date across all stored conversation files,
    /// or nil if no messages are stored locally.
    func newestMessageDate() -> Date? {
        guard let items = try? fm.contentsOfDirectory(at: documentsURL,
                                                       includingPropertiesForKeys: nil) else { return nil }
        var newest: Date? = nil
        for url in items where url.lastPathComponent.hasPrefix("msgs_") && url.pathExtension == "json" {
            guard let msgs = read([StoredMessage].self, from: url) else { continue }
            for m in msgs {
                if newest == nil || m.createdAt > newest! {
                    newest = m.createdAt
                }
            }
        }
        return newest
    }

    /// Returns the ID of the oldest stored message for the given conversation,
    /// or nil if no messages exist locally for that contact.
    func oldestMessageID(forKey identityKey: String) -> String? {
        let msgs = loadMessages(forKey: identityKey)
        return msgs.min(by: { $0.createdAt < $1.createdAt })?.id
    }

    // MARK: - Helpers

    private let encoder: JSONEncoder = {
        let e = JSONEncoder()
        e.dateEncodingStrategy = .iso8601
        return e
    }()

    private let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .iso8601
        return d
    }()

    private func write<T: Encodable>(_ value: T, to url: URL) {
        do {
            let data = try encoder.encode(value)
            try data.write(to: url, options: .atomic)
        } catch {
            print("LocalStore write error at \(url.lastPathComponent): \(error)")
        }
    }

    private func read<T: Decodable>(_ type: T.Type, from url: URL) -> T? {
        guard fm.fileExists(atPath: url.path) else { return nil }
        do {
            let data = try Data(contentsOf: url)
            return try decoder.decode(type, from: data)
        } catch {
            print("LocalStore read error at \(url.lastPathComponent): \(error)")
            return nil
        }
    }
}
