import Foundation

/// Persists nexuses and per-nexus message history as JSON files in the
/// app's Documents directory.
final class LocalStore {

    private let fm = FileManager.default

    private var documentsURL: URL {
        fm.urls(for: .documentDirectory, in: .userDomainMask)[0]
    }

    // MARK: - Username

    private var usernameURL: URL {
        documentsURL.appendingPathComponent("username.json")
    }

    func saveUsername(_ username: String) {
        write(username, to: usernameURL)
    }

    func loadUsername() -> String? {
        read(String.self, from: usernameURL)
    }

    // MARK: - Nexuses

    private var nexusesURL: URL {
        documentsURL.appendingPathComponent("nexuses.json")
    }

    func saveNexuses(_ nexuses: [Nexus]) {
        write(nexuses, to: nexusesURL)
    }

    func loadNexuses() -> [Nexus] {
        read([Nexus].self, from: nexusesURL) ?? []
    }

    // MARK: - Messages

    private func messagesURL(forNexus nexusId: String) -> URL {
        let safe = nexusId.replacingOccurrences(of: "/", with: "_")
                          .replacingOccurrences(of: "+", with: "-")
        return documentsURL.appendingPathComponent("msgs_\(safe).json")
    }

    func saveMessages(_ messages: [StoredMessage], forNexus nexusId: String) {
        write(messages, to: messagesURL(forNexus: nexusId))
    }

    func loadMessages(forNexus nexusId: String) -> [StoredMessage] {
        read([StoredMessage].self, from: messagesURL(forNexus: nexusId)) ?? []
    }

    func appendMessage(_ message: StoredMessage, forNexus nexusId: String) {
        var existing = loadMessages(forNexus: nexusId)
        guard !existing.contains(where: { $0.id == message.id }) else { return }
        existing.append(message)
        saveMessages(existing, forNexus: nexusId)
    }

    func newestMessageDate(forNexus nexusId: String) -> Date? {
        let msgs = loadMessages(forNexus: nexusId)
        return msgs.max(by: { $0.createdAt < $1.createdAt })?.createdAt
    }

    func oldestMessageID(forNexus nexusId: String) -> String? {
        let msgs = loadMessages(forNexus: nexusId)
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
