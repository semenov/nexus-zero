import Foundation

// MARK: - Response types

struct UserResponse: Codable {
    let identityKey: String
    let encryptionKey: String
    let createdAt: Date
}

struct ContactResponse: Codable {
    let contactKey: String
    let nickname: String
    let updatedAt: Date
}

struct MessageResponse: Codable {
    let id: String
    let senderKey: String
    let recipientKey: String?
    let ephemeralKey: String
    let ciphertext: String
    let senderEphemeralKey: String?
    let senderCiphertext: String?
    let createdAt: Date
}

struct SendMessageResponse: Codable {
    let id: String
    let createdAt: Date
}

// MARK: - Errors

enum APIError: LocalizedError {
    case invalidURL
    case httpError(Int, String)
    case decodingError(Error)
    case networkError(Error)

    var errorDescription: String? {
        switch self {
        case .invalidURL:               return "Invalid URL"
        case .httpError(let c, let m):  return "HTTP \(c): \(m)"
        case .decodingError(let e):     return "Decode error: \(e.localizedDescription)"
        case .networkError(let e):      return "Network error: \(e.localizedDescription)"
        }
    }
}

// MARK: - APIClient

/// Communicates with the messenger backend over HTTPS.
final class APIClient {

    let baseURL: String
    let keyManager: KeyManager

    private let session: URLSession
    private let decoder: JSONDecoder
    private let encoder: JSONEncoder

    init(baseURL: String, keyManager: KeyManager) {
        self.baseURL = baseURL.hasSuffix("/") ? String(baseURL.dropLast()) : baseURL
        self.keyManager = keyManager

        let cfg = URLSessionConfiguration.default
        cfg.timeoutIntervalForRequest = 30
        self.session = URLSession(configuration: cfg)

        self.decoder = JSONDecoder()
        self.decoder.keyDecodingStrategy = .convertFromSnakeCase
        // The server emits timestamps with fractional seconds and a timezone
        // offset (e.g. "2026-03-25T03:06:40.714976+03:00").  Swift's plain
        // .iso8601 strategy does not handle fractional seconds, so we use a
        // custom formatter that does.
        let iso = ISO8601DateFormatter()
        iso.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        self.decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let str = try container.decode(String.self)
            if let date = iso.date(from: str) { return date }
            // Fallback: no fractional seconds
            iso.formatOptions = [.withInternetDateTime]
            defer { iso.formatOptions = [.withInternetDateTime, .withFractionalSeconds] }
            if let date = iso.date(from: str) { return date }
            throw DecodingError.dataCorruptedError(in: container,
                debugDescription: "Cannot parse date: \(str)")
        }

        self.encoder = JSONEncoder()
        self.encoder.keyEncodingStrategy = .convertToSnakeCase
        self.encoder.dateEncodingStrategy = .iso8601
    }

    // MARK: - Endpoints

    /// Registers the current device with the server.
    func registerUser() async throws {
        let body: [String: String] = [
            "identity_key": keyManager.identityKeyString,
            "encryption_key": keyManager.encryptionKeyString,
        ]
        var req = try makeRequest(path: "/v1/users", method: "POST", authenticated: false)
        req.httpBody = try JSONSerialization.data(withJSONObject: body)
        let (_, response) = try await perform(req)
        let code = (response as! HTTPURLResponse).statusCode
        // 201 = created, 409 = already registered (idempotent — treat as success)
        guard code == 201 || code == 409 else {
            throw APIError.httpError(code, "registration failed")
        }
    }

    /// Fetches the public profile of a user by their identity key.
    func getUser(identityKey: String) async throws -> UserResponse {
        let encoded = identityKey.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? identityKey
        let req = try makeRequest(path: "/v1/users/\(encoded)", method: "GET", authenticated: false)
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data)
        return try decode(UserResponse.self, from: data)
    }

    /// Sends an encrypted message to a recipient, including a sender copy for history.
    /// Returns the server-assigned message ID and timestamp.
    func sendMessage(recipientKey: String, ephemeralKey: String, ciphertext: String,
                     senderEphemeralKey: String, senderCiphertext: String) async throws -> SendMessageResponse {
        let body: [String: String] = [
            "recipient_key": recipientKey,
            "ephemeral_key": ephemeralKey,
            "ciphertext": ciphertext,
            "sender_ephemeral_key": senderEphemeralKey,
            "sender_ciphertext": senderCiphertext,
        ]
        var req = try makeRequest(path: "/v1/messages", method: "POST", authenticated: true)
        req.httpBody = try JSONSerialization.data(withJSONObject: body)
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data, expected: 201)
        return try decode(SendMessageResponse.self, from: data)
    }

    /// Fetches paginated message history (sent + received) for the authenticated user.
    /// - Parameters:
    ///   - limit: Maximum number of messages to return (default 100, server max 500).
    ///   - since: If set, only messages with created_at > since are returned (incremental sync).
    ///   - beforeID: If set, returns up to `limit` messages older than this message ID.
    func getHistory(limit: Int = 100, since: Date? = nil, beforeID: String? = nil) async throws -> [MessageResponse] {
        var components = URLComponents(string: baseURL + "/v1/messages/history")!
        var queryItems: [URLQueryItem] = [URLQueryItem(name: "limit", value: String(limit))]
        if let since = since {
            let iso = ISO8601DateFormatter()
            iso.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            queryItems.append(URLQueryItem(name: "since", value: iso.string(from: since)))
        }
        if let beforeID = beforeID {
            queryItems.append(URLQueryItem(name: "before_id", value: beforeID))
        }
        components.queryItems = queryItems
        guard let url = components.url else { throw APIError.invalidURL }
        var req = URLRequest(url: url)
        req.httpMethod = "GET"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.setValue(try keyManager.authHeader(), forHTTPHeaderField: "Authorization")
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data)
        return try decode([MessageResponse].self, from: data)
    }

    /// Fetches all server-side contacts for the authenticated user.
    func getContacts() async throws -> [ContactResponse] {
        let req = try makeRequest(path: "/v1/contacts", method: "GET", authenticated: true)
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data)
        return try decode([ContactResponse].self, from: data)
    }

    /// Creates or updates a contact on the server.
    func upsertContact(key: String, nickname: String) async throws {
        let encoded = key.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? key
        var req = try makeRequest(path: "/v1/contacts/\(encoded)", method: "PUT", authenticated: true)
        req.httpBody = try JSONSerialization.data(withJSONObject: ["nickname": nickname])
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data, expected: 204)
    }

    /// Deletes a contact from the server.
    func deleteContact(key: String) async throws {
        let encoded = key.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? key
        let req = try makeRequest(path: "/v1/contacts/\(encoded)", method: "DELETE", authenticated: true)
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data, expected: 204)
    }

    /// Retrieves (and marks as delivered) all pending messages for the
    /// authenticated user.
    func getPendingMessages() async throws -> [MessageResponse] {
        let req = try makeRequest(path: "/v1/messages/pending", method: "GET", authenticated: true)
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data)
        return try decode([MessageResponse].self, from: data)
    }

    // MARK: - Private helpers

    private func makeRequest(path: String, method: String, authenticated: Bool) throws -> URLRequest {
        guard let url = URL(string: baseURL + path) else { throw APIError.invalidURL }
        var req = URLRequest(url: url)
        req.httpMethod = method
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if authenticated {
            req.setValue(try keyManager.authHeader(), forHTTPHeaderField: "Authorization")
        }
        return req
    }

    private func perform(_ request: URLRequest) async throws -> (Data, URLResponse) {
        do {
            return try await session.data(for: request)
        } catch {
            throw APIError.networkError(error)
        }
    }

    private func assertSuccess(_ response: URLResponse, data: Data, expected: Int = 200) throws {
        let code = (response as! HTTPURLResponse).statusCode
        if code == expected { return }
        // Attempt to extract the server's error message.
        if let errBody = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
           let msg = errBody["error"] as? String {
            throw APIError.httpError(code, msg)
        }
        throw APIError.httpError(code, HTTPURLResponse.localizedString(forStatusCode: code))
    }

    private func decode<T: Decodable>(_ type: T.Type, from data: Data) throws -> T {
        do {
            return try decoder.decode(type, from: data)
        } catch {
            throw APIError.decodingError(error)
        }
    }
}
