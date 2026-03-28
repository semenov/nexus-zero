import Foundation

/// Maintains a persistent WebSocket connection to the server and delivers
/// incoming messages and member events to the registered callbacks.
///
/// Reconnection uses exponential back-off capped at 30 seconds.
final class WebSocketClient: ObservableObject {

    /// Called on the main actor whenever a new message frame arrives.
    var onMessage: ((MessageResponse) -> Void)?
    /// Called on the main actor whenever a membership event arrives.
    var onMemberEvent: ((MemberEventFrame) -> Void)?

    private var task: URLSessionWebSocketTask?
    private var session: URLSession?
    private var currentBaseURL: String = ""
    private var currentAuthHeader: String = ""
    private var isConnected: Bool = false
    private var shouldReconnect: Bool = false
    private var backoffDelay: TimeInterval = 1
    private let maxBackoff: TimeInterval = 30

    private let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.keyDecodingStrategy = .convertFromSnakeCase
        let iso = ISO8601DateFormatter()
        iso.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        d.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let str = try container.decode(String.self)
            if let date = iso.date(from: str) { return date }
            iso.formatOptions = [.withInternetDateTime]
            defer { iso.formatOptions = [.withInternetDateTime, .withFractionalSeconds] }
            if let date = iso.date(from: str) { return date }
            throw DecodingError.dataCorruptedError(in: container,
                debugDescription: "Cannot parse date: \(str)")
        }
        return d
    }()

    // MARK: - Public API

    func connect(baseURL: String, authHeader: String) {
        currentBaseURL = baseURL
        currentAuthHeader = authHeader
        shouldReconnect = true
        backoffDelay = 1
        openConnection()
    }

    func disconnect() {
        shouldReconnect = false
        task?.cancel(with: .goingAway, reason: nil)
        task = nil
    }

    // MARK: - Connection management

    private func openConnection() {
        guard let url = buildWebSocketURL() else {
            print("WS: failed to build URL")
            return
        }
        print("WS: connecting to \(url)")
        let cfg = URLSessionConfiguration.default
        session = URLSession(configuration: cfg)
        task = session?.webSocketTask(with: url)
        task?.resume()
        isConnected = true
        backoffDelay = 1
        receiveLoop()
    }

    private func buildWebSocketURL() -> URL? {
        let token: String
        if currentAuthHeader.hasPrefix("Ed25519 ") {
            token = String(currentAuthHeader.dropFirst("Ed25519 ".count))
        } else {
            token = currentAuthHeader
        }
        guard let encodedToken = token.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) else {
            return nil
        }

        var wsBase = currentBaseURL
        if wsBase.hasPrefix("https://") {
            wsBase = "wss://" + wsBase.dropFirst("https://".count)
        } else if wsBase.hasPrefix("http://") {
            wsBase = "ws://" + wsBase.dropFirst("http://".count)
        }
        let urlString = "\(wsBase)/v1/ws?auth=\(encodedToken)"
        return URL(string: urlString)
    }

    // MARK: - Receive loop

    private func receiveLoop() {
        task?.receive { [weak self] result in
            guard let self else { return }
            switch result {
            case .success(let message):
                self.handleMessage(message)
                self.receiveLoop()
            case .failure:
                self.handleDisconnect()
            }
        }
    }

    private func handleMessage(_ message: URLSessionWebSocketTask.Message) {
        let data: Data
        switch message {
        case .data(let d):   data = d
        case .string(let s): data = Data(s.utf8)
        @unknown default:    return
        }

        print("WS: received frame: \(String(data: data, encoding: .utf8) ?? "<binary>")")

        guard let envelope = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let type_ = envelope["type"] as? String else {
            print("WS: failed to parse envelope")
            return
        }

        if type_ == "message" {
            do {
                let msg = try decoder.decode(MessageResponse.self, from: data)
                print("WS: decoded message id=\(msg.id)")
                DispatchQueue.main.async {
                    self.onMessage?(msg)
                }
                sendAck(id: msg.id)
            } catch {
                print("WS: decode error: \(error)")
            }
        } else if type_ == "member_joined" || type_ == "member_left" || type_ == "member_kicked" {
            let event = MemberEventFrame(
                type: type_,
                nexusId: envelope["nexus_id"] as? String ?? "",
                identityKey: envelope["identity_key"] as? String ?? "",
                username: envelope["username"] as? String
            )
            DispatchQueue.main.async {
                self.onMemberEvent?(event)
            }
        }
    }

    private func sendAck(id: String) {
        let ack = ["type": "ack", "id": id]
        guard let data = try? JSONSerialization.data(withJSONObject: ack),
              let str = String(data: data, encoding: .utf8) else { return }
        task?.send(.string(str)) { _ in }
    }

    // MARK: - Reconnection

    private func handleDisconnect() {
        print("WS: disconnected, shouldReconnect=\(shouldReconnect)")
        isConnected = false
        guard shouldReconnect else { return }

        let delay = backoffDelay
        backoffDelay = min(backoffDelay * 2, maxBackoff)

        DispatchQueue.global().asyncAfter(deadline: .now() + delay) { [weak self] in
            guard let self, self.shouldReconnect else { return }
            self.openConnection()
        }
    }
}

// MARK: - Member event frame

struct MemberEventFrame {
    let type: String
    let nexusId: String
    let identityKey: String
    let username: String?
}
