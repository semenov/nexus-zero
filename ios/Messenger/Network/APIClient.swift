import Foundation

// MARK: - Response types

struct UserResponse: Codable {
    let identityKey: String
    let encryptionKey: String
    let username: String?
    let createdAt: Date
}

struct NexusResponse: Codable {
    let id: String
    let name: String
    let creatorKey: String
    let createdAt: Date
    let role: String
}

struct NexusDetailResponse: Codable {
    let id: String
    let name: String
    let creatorKey: String
    let createdAt: Date
    let role: String
    let members: [NexusMemberResponse]
}

struct NexusMemberResponse: Codable {
    let identityKey: String
    let username: String?
    let encryptionKey: String
    let role: String
    let joinedAt: Date
}

struct InviteCodeResponse: Codable {
    let id: String
    let nexusId: String
    let code: String
    let createdBy: String
    let maxUses: Int?
    let useCount: Int
    let revoked: Bool
    let createdAt: Date
    let expiresAt: Date?
}

struct MessageResponse: Codable {
    let id: String
    let nexusId: String
    let senderKey: String
    let recipientKey: String
    let ephemeralKey: String
    let ciphertext: String
    let senderUsername: String?
    let createdAt: Date
}

struct SendMessageResponse: Codable {
    let id: String
    let createdAt: Date
}

struct JoinResponse: Codable {
    let nexusId: String
    let name: String
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
        let iso = ISO8601DateFormatter()
        iso.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        self.decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let str = try container.decode(String.self)
            if let date = iso.date(from: str) { return date }
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

    // MARK: - Users

    func registerUser() async throws {
        let body: [String: String] = [
            "identity_key": keyManager.identityKeyString,
            "encryption_key": keyManager.encryptionKeyString,
        ]
        var req = try makeRequest(path: "/v1/users", method: "POST", authenticated: false)
        req.httpBody = try JSONSerialization.data(withJSONObject: body)
        let (_, response) = try await perform(req)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw APIError.networkError(URLError(.badServerResponse))
        }
        let code = httpResponse.statusCode
        guard code == 201 || code == 409 else {
            throw APIError.httpError(code, "registration failed")
        }
    }

    func getUser(identityKey: String) async throws -> UserResponse {
        let encoded = identityKey.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? identityKey
        let req = try makeRequest(path: "/v1/users/\(encoded)", method: "GET", authenticated: false)
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data)
        return try decode(UserResponse.self, from: data)
    }

    func setUsername(_ username: String) async throws {
        var req = try makeRequest(path: "/v1/users/me/username", method: "PUT", authenticated: true)
        req.httpBody = try JSONSerialization.data(withJSONObject: ["username": username])
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data, expected: 204)
    }

    // MARK: - Nexuses

    func createNexus(name: String) async throws -> NexusResponse {
        var req = try makeRequest(path: "/v1/nexuses", method: "POST", authenticated: true)
        req.httpBody = try JSONSerialization.data(withJSONObject: ["name": name])
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data, expected: 201)
        return try decode(NexusResponse.self, from: data)
    }

    func getNexuses() async throws -> [NexusResponse] {
        let req = try makeRequest(path: "/v1/nexuses", method: "GET", authenticated: true)
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data)
        return try decode([NexusResponse].self, from: data)
    }

    func getNexus(id: String) async throws -> NexusDetailResponse {
        let req = try makeRequest(path: "/v1/nexuses/\(id)", method: "GET", authenticated: true)
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data)
        return try decode(NexusDetailResponse.self, from: data)
    }

    func updateNexus(id: String, name: String) async throws {
        var req = try makeRequest(path: "/v1/nexuses/\(id)", method: "PUT", authenticated: true)
        req.httpBody = try JSONSerialization.data(withJSONObject: ["name": name])
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data, expected: 204)
    }

    func deleteNexus(id: String) async throws {
        let req = try makeRequest(path: "/v1/nexuses/\(id)", method: "DELETE", authenticated: true)
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data, expected: 204)
    }

    // MARK: - Members

    func getMembers(nexusId: String) async throws -> [NexusMemberResponse] {
        let req = try makeRequest(path: "/v1/nexuses/\(nexusId)/members", method: "GET", authenticated: true)
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data)
        return try decode([NexusMemberResponse].self, from: data)
    }

    func kickMember(nexusId: String, identityKey: String) async throws {
        let encoded = identityKey.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? identityKey
        let req = try makeRequest(path: "/v1/nexuses/\(nexusId)/members/\(encoded)", method: "DELETE", authenticated: true)
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data, expected: 204)
    }

    func leaveNexus(id: String) async throws {
        let req = try makeRequest(path: "/v1/nexuses/\(id)/leave", method: "POST", authenticated: true)
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data, expected: 204)
    }

    // MARK: - Invites

    func createInvite(nexusId: String, maxUses: Int? = nil, expiresInHours: Int? = nil) async throws -> InviteCodeResponse {
        var body: [String: Any] = [:]
        if let m = maxUses { body["max_uses"] = m }
        if let e = expiresInHours { body["expires_in_hours"] = e }
        var req = try makeRequest(path: "/v1/nexuses/\(nexusId)/invites", method: "POST", authenticated: true)
        req.httpBody = try JSONSerialization.data(withJSONObject: body)
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data, expected: 201)
        return try decode(InviteCodeResponse.self, from: data)
    }

    func getInvites(nexusId: String) async throws -> [InviteCodeResponse] {
        let req = try makeRequest(path: "/v1/nexuses/\(nexusId)/invites", method: "GET", authenticated: true)
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data)
        return try decode([InviteCodeResponse].self, from: data)
    }

    func revokeInvite(nexusId: String, inviteId: String) async throws {
        let req = try makeRequest(path: "/v1/nexuses/\(nexusId)/invites/\(inviteId)", method: "DELETE", authenticated: true)
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data, expected: 204)
    }

    // MARK: - Join

    func joinNexus(code: String) async throws -> JoinResponse {
        var req = try makeRequest(path: "/v1/join", method: "POST", authenticated: true)
        req.httpBody = try JSONSerialization.data(withJSONObject: ["code": code])
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data)
        return try decode(JoinResponse.self, from: data)
    }

    // MARK: - Messages

    struct Envelope: Codable {
        let recipientKey: String
        let ephemeralKey: String
        let ciphertext: String
    }

    func sendNexusMessage(nexusId: String, envelopes: [Envelope]) async throws -> SendMessageResponse {
        let body: [[String: String]] = envelopes.map {
            ["recipient_key": $0.recipientKey, "ephemeral_key": $0.ephemeralKey, "ciphertext": $0.ciphertext]
        }
        var req = try makeRequest(path: "/v1/nexuses/\(nexusId)/messages", method: "POST", authenticated: true)
        req.httpBody = try JSONSerialization.data(withJSONObject: ["envelopes": body])
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data, expected: 201)
        return try decode(SendMessageResponse.self, from: data)
    }

    func getNexusHistory(nexusId: String, limit: Int = 100, since: Date? = nil, beforeId: String? = nil) async throws -> [MessageResponse] {
        var components = URLComponents(string: baseURL + "/v1/nexuses/\(nexusId)/messages")!
        var queryItems: [URLQueryItem] = [URLQueryItem(name: "limit", value: String(limit))]
        if let since = since {
            let iso = ISO8601DateFormatter()
            iso.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            queryItems.append(URLQueryItem(name: "since", value: iso.string(from: since)))
        }
        if let beforeId = beforeId {
            queryItems.append(URLQueryItem(name: "before_id", value: beforeId))
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

    func getPendingMessages() async throws -> [MessageResponse] {
        let req = try makeRequest(path: "/v1/messages/pending", method: "GET", authenticated: true)
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data)
        return try decode([MessageResponse].self, from: data)
    }

    // MARK: - Device Token

    func registerDeviceToken(_ token: String) async throws {
        var req = try makeRequest(path: "/v1/device-token", method: "PUT", authenticated: true)
        req.httpBody = try JSONSerialization.data(withJSONObject: ["token": token])
        let (data, response) = try await perform(req)
        try assertSuccess(response, data: data, expected: 204)
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
        guard let httpResponse = response as? HTTPURLResponse else {
            throw APIError.networkError(URLError(.badServerResponse))
        }
        let code = httpResponse.statusCode
        if code == expected { return }
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
