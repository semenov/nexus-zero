import Foundation
import SwiftUI
import UIKit
import UserNotifications

/// Central application state, owns all subsystems and drives cross-cutting
/// workflows.
@MainActor
final class AppState: ObservableObject {

    // MARK: - Published state

    @Published var nexuses: [Nexus] = []
    @Published var conversations: [String: [StoredMessage]] = [:]
    @Published var username: String? = nil
    var activeNexusId: String? = nil
    @Published var pendingOpenNexusId: String? = nil
    @Published var hasMoreHistory: [String: Bool] = [:]

    // MARK: - Subsystems

    let keyManager = KeyManager()
    let localStore = LocalStore()
    private(set) lazy var cryptoEngine = CryptoEngine(keyManager: keyManager)
    private(set) lazy var apiClient = APIClient(baseURL: serverBaseURL, keyManager: keyManager)
    private(set) lazy var wsClient = WebSocketClient()

    // MARK: - Configuration

    private let serverBaseURL = "https://nexus.semenov.ai"

    // MARK: - Derived state

    var isOnboarded: Bool {
        keyManager.signingKey != nil && keyManager.agreementKey != nil
    }

    var hasUsername: Bool {
        username != nil && !(username?.isEmpty ?? true)
    }

    // MARK: - Setup

    func setup() {
        // Load persisted data.
        nexuses = localStore.loadNexuses()
        username = localStore.loadUsername()

        for nexus in nexuses {
            conversations[nexus.id] = localStore.loadMessages(forNexus: nexus.id)
        }

        // Request notification permission.
        Task {
            let granted = (try? await UNUserNotificationCenter.current()
                .requestAuthorization(options: [.alert, .sound, .badge])) ?? false
            if granted {
                await MainActor.run {
                    UIApplication.shared.registerForRemoteNotifications()
                }
            }
        }

        connectWebSocket()

        Task {
            await syncNexusesFromServer()
            await fetchPendingMessages()
        }
    }

    // MARK: - Username

    func chooseUsername(_ name: String) async throws {
        try await apiClient.setUsername(name)
        username = name
        localStore.saveUsername(name)
    }

    /// Checks server for existing username (e.g. after restore).
    func checkExistingUsername() async {
        guard let user = try? await apiClient.getUser(identityKey: keyManager.identityKeyString) else { return }
        if let serverUsername = user.username, !serverUsername.isEmpty {
            username = serverUsername
            localStore.saveUsername(serverUsername)
        }
    }

    // MARK: - Push notifications

    func registerDeviceToken(_ tokenData: Data) {
        let token = tokenData.map { String(format: "%02x", $0) }.joined()
        Task {
            try? await apiClient.registerDeviceToken(token)
        }
    }

    // MARK: - Nexus management

    func createNexus(name: String) async throws -> Nexus {
        let resp = try await apiClient.createNexus(name: name)
        let nexus = Nexus(
            id: resp.id,
            name: resp.name,
            creatorKey: resp.creatorKey,
            role: resp.role,
            members: [NexusMember(
                identityKey: keyManager.identityKeyString,
                username: username,
                encryptionKey: keyManager.encryptionKeyString,
                role: "admin"
            )]
        )
        nexuses.append(nexus)
        conversations[nexus.id] = []
        localStore.saveNexuses(nexuses)
        return nexus
    }

    func joinNexus(code: String) async throws -> Nexus {
        let resp = try await apiClient.joinNexus(code: code)
        // Fetch full detail to get members.
        let detail = try await apiClient.getNexus(id: resp.nexusId)
        let nexus = Nexus(
            id: detail.id,
            name: detail.name,
            creatorKey: detail.creatorKey,
            role: detail.role,
            members: detail.members.map {
                NexusMember(identityKey: $0.identityKey, username: $0.username,
                            encryptionKey: $0.encryptionKey, role: $0.role)
            }
        )
        if !nexuses.contains(where: { $0.id == nexus.id }) {
            nexuses.append(nexus)
            conversations[nexus.id] = []
            localStore.saveNexuses(nexuses)
        }
        return nexus
    }

    func leaveNexus(id: String) async throws {
        try await apiClient.leaveNexus(id: id)
        nexuses.removeAll { $0.id == id }
        conversations.removeValue(forKey: id)
        localStore.saveNexuses(nexuses)
    }

    func deleteNexus(id: String) async throws {
        try await apiClient.deleteNexus(id: id)
        nexuses.removeAll { $0.id == id }
        conversations.removeValue(forKey: id)
        localStore.saveNexuses(nexuses)
    }

    func kickMember(nexusId: String, identityKey: String) async throws {
        try await apiClient.kickMember(nexusId: nexusId, identityKey: identityKey)
        if let idx = nexuses.firstIndex(where: { $0.id == nexusId }) {
            nexuses[idx].members.removeAll { $0.identityKey == identityKey }
            localStore.saveNexuses(nexuses)
        }
    }

    func createInvite(nexusId: String) async throws -> InviteCodeResponse {
        return try await apiClient.createInvite(nexusId: nexusId)
    }

    func refreshNexusMembers(nexusId: String) async {
        guard let detail = try? await apiClient.getNexus(id: nexusId) else { return }
        if let idx = nexuses.firstIndex(where: { $0.id == nexusId }) {
            nexuses[idx].members = detail.members.map {
                NexusMember(identityKey: $0.identityKey, username: $0.username,
                            encryptionKey: $0.encryptionKey, role: $0.role)
            }
            localStore.saveNexuses(nexuses)
        }
    }

    // MARK: - Sync nexuses from server

    private func syncNexusesFromServer() async {
        guard let serverNexuses = try? await apiClient.getNexuses() else { return }

        // Remove local nexuses that are no longer on the server.
        let serverIds = Set(serverNexuses.map(\.id))
        nexuses.removeAll { !serverIds.contains($0.id) }

        for sn in serverNexuses {
            if let idx = nexuses.firstIndex(where: { $0.id == sn.id }) {
                // Update name/role if changed.
                if nexuses[idx].name != sn.name || nexuses[idx].role != sn.role {
                    nexuses[idx] = Nexus(id: sn.id, name: sn.name, creatorKey: sn.creatorKey,
                                         role: sn.role, members: nexuses[idx].members)
                }
            } else {
                let nexus = Nexus(id: sn.id, name: sn.name, creatorKey: sn.creatorKey,
                                  role: sn.role, members: [])
                nexuses.append(nexus)
                conversations[nexus.id] = localStore.loadMessages(forNexus: nexus.id)
            }
        }
        localStore.saveNexuses(nexuses)

        // Refresh members for each nexus.
        for nexus in nexuses {
            await refreshNexusMembers(nexusId: nexus.id)
        }

        // Fetch history for each nexus.
        for nexus in nexuses {
            await fetchNexusHistory(nexusId: nexus.id)
        }
    }

    // MARK: - Send message

    func sendMessage(nexusId: String, text: String) async throws {
        guard let nexus = nexuses.first(where: { $0.id == nexusId }) else { return }

        // Encrypt for every member (fan-out).
        var envelopes: [APIClient.Envelope] = []
        for member in nexus.members {
            guard !member.encryptionKey.isEmpty else { continue }
            let (ephemeralKey, ciphertext) = try cryptoEngine.encrypt(
                text: text,
                recipientEncryptionKey: member.encryptionKey
            )
            envelopes.append(APIClient.Envelope(
                recipientKey: member.identityKey,
                ephemeralKey: ephemeralKey,
                ciphertext: ciphertext
            ))
        }

        let serverResponse = try await apiClient.sendNexusMessage(nexusId: nexusId, envelopes: envelopes)

        let stored = StoredMessage(
            id: serverResponse.id,
            nexusId: nexusId,
            senderKey: keyManager.identityKeyString,
            senderUsername: username,
            text: text,
            createdAt: serverResponse.createdAt,
            isOutgoing: true
        )
        appendToConversation(stored, nexusId: nexusId)
    }

    // MARK: - WebSocket

    private func connectWebSocket() {
        guard let authHeader = try? keyManager.authHeader() else { return }

        wsClient.onMessage = { [weak self] response in
            Task { @MainActor [weak self] in
                self?.handleIncoming(response)
            }
        }

        wsClient.onMemberEvent = { [weak self] event in
            Task { @MainActor [weak self] in
                self?.handleMemberEvent(event)
            }
        }

        wsClient.connect(baseURL: serverBaseURL, authHeader: authHeader)
    }

    private func handleIncoming(_ response: MessageResponse) {
        let senderKey = response.senderKey
        let nexusId = response.nexusId

        // Skip WS echo of our own messages — already added optimistically on send.
        if senderKey == keyManager.identityKeyString { return }

        // Decrypt the message (it's encrypted for us).
        let text: String
        do {
            text = try cryptoEngine.decrypt(ephemeralKey: response.ephemeralKey, ciphertext: response.ciphertext)
        } catch {
            print("AppState: decryption failed for message \(response.id): \(error)")
            return
        }

        let stored = StoredMessage(
            id: response.id,
            nexusId: nexusId,
            senderKey: senderKey,
            senderUsername: response.senderUsername,
            text: text,
            createdAt: response.createdAt,
            isOutgoing: false
        )
        appendToConversation(stored, nexusId: nexusId)

        if activeNexusId != nexusId {
            let senderName = response.senderUsername ?? String(senderKey.prefix(8)) + "…"
            let nexusName = nexuses.first(where: { $0.id == nexusId })?.name ?? "Unknown"
            scheduleLocalNotification(from: senderName, nexus: nexusName, text: text, id: response.id, nexusId: nexusId)
        }
    }

    private func handleMemberEvent(_ event: MemberEventFrame) {
        let nexusId = event.nexusId

        if event.type == "member_kicked" && event.identityKey == keyManager.identityKeyString {
            // We were kicked.
            nexuses.removeAll { $0.id == nexusId }
            conversations.removeValue(forKey: nexusId)
            localStore.saveNexuses(nexuses)
            return
        }

        // Refresh members list for any member event.
        Task {
            await refreshNexusMembers(nexusId: nexusId)
        }
    }

    private func scheduleLocalNotification(from sender: String, nexus: String, text: String, id: String, nexusId: String) {
        let content = UNMutableNotificationContent()
        content.title = "\(sender) in \(nexus)"
        content.body = text
        content.sound = .default
        content.userInfo = ["nexus_id": nexusId]
        let request = UNNotificationRequest(identifier: id, content: content, trigger: nil)
        UNUserNotificationCenter.current().add(request)
    }

    // MARK: - History fetch

    private func fetchNexusHistory(nexusId: String) async {
        let since = localStore.newestMessageDate(forNexus: nexusId)
        guard let messages = try? await apiClient.getNexusHistory(nexusId: nexusId, limit: 100, since: since) else { return }
        for response in messages {
            processHistoryMessage(response)
        }
    }

    func loadOlderMessages(nexusId: String) async {
        let oldestID = localStore.oldestMessageID(forNexus: nexusId)
        let limit = 50
        guard let messages = try? await apiClient.getNexusHistory(nexusId: nexusId, limit: limit, beforeId: oldestID) else { return }
        for response in messages.reversed() {
            processHistoryMessage(response, prepending: true)
        }
        if messages.count < limit {
            hasMoreHistory[nexusId] = false
        }
    }

    private func processHistoryMessage(_ response: MessageResponse, prepending: Bool = false) {
        let isOutgoing = response.senderKey == keyManager.identityKeyString

        let text: String
        do {
            text = try cryptoEngine.decrypt(ephemeralKey: response.ephemeralKey, ciphertext: response.ciphertext)
        } catch {
            print("AppState: history decryption failed for \(response.id): \(error)")
            return
        }

        let stored = StoredMessage(
            id: response.id,
            nexusId: response.nexusId,
            senderKey: response.senderKey,
            senderUsername: response.senderUsername,
            text: text,
            createdAt: response.createdAt,
            isOutgoing: isOutgoing
        )
        if prepending {
            prependToConversation(stored, nexusId: response.nexusId)
        } else {
            appendToConversation(stored, nexusId: response.nexusId)
        }
    }

    // MARK: - Pending messages

    private func fetchPendingMessages() async {
        guard let messages = try? await apiClient.getPendingMessages() else { return }
        for response in messages {
            handleIncoming(response)
        }
    }

    // MARK: - Helpers

    private func appendToConversation(_ message: StoredMessage, nexusId: String) {
        var thread = conversations[nexusId] ?? []
        guard !thread.contains(where: { $0.id == message.id }) else { return }
        thread.append(message)
        conversations[nexusId] = thread
        localStore.appendMessage(message, forNexus: nexusId)
    }

    private func prependToConversation(_ message: StoredMessage, nexusId: String) {
        var thread = conversations[nexusId] ?? []
        guard !thread.contains(where: { $0.id == message.id }) else { return }
        thread.insert(message, at: 0)
        conversations[nexusId] = thread
        localStore.appendMessage(message, forNexus: nexusId)
    }
}
