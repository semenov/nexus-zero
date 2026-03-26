import Foundation
import SwiftUI
import UserNotifications

/// Central application state, owns all subsystems and drives cross-cutting
/// workflows.
@MainActor
final class AppState: ObservableObject {

    // MARK: - Published state

    @Published var contacts: [Contact] = []
    @Published var conversations: [String: [StoredMessage]] = [:]
    var activeChatKey: String? = nil
    @Published var pendingOpenContactKey: String? = nil
    /// Tracks whether there may be more older messages available for each conversation.
    /// Defaults to true (unknown) until a page returns fewer messages than requested.
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

    // MARK: - Setup

    /// Call once on launch (after keys exist) to load persisted data and open
    /// the WebSocket connection.
    func setup() {
        // Load persisted contacts.
        contacts = localStore.loadContacts()

        // Load persisted conversations for each contact.
        for contact in contacts {
            conversations[contact.identityKey] = localStore.loadMessages(forKey: contact.identityKey)
        }

        // Request notification permission (no-op if already granted/denied).
        Task {
            try? await UNUserNotificationCenter.current()
                .requestAuthorization(options: [.alert, .sound, .badge])
        }

        // Connect WebSocket for real-time delivery.
        connectWebSocket()

        // Sync contacts from server, then load full history, then pending.
        Task {
            await syncContactsFromServer()
            await fetchHistory()
            await fetchPendingMessages()
        }
    }

    // MARK: - Contacts

    func addContact(_ contact: Contact) {
        guard !contacts.contains(where: { $0.identityKey == contact.identityKey }) else { return }
        contacts.append(contact)
        localStore.saveContacts(contacts)
        if conversations[contact.identityKey] == nil {
            let stored = localStore.loadMessages(forKey: contact.identityKey)
            conversations[contact.identityKey] = stored.isEmpty ? [] : stored
        }
        Task { try? await apiClient.upsertContact(key: contact.identityKey, nickname: contact.nickname) }
    }

    func renameContact(_ contact: Contact, to nickname: String) {
        guard let idx = contacts.firstIndex(where: { $0.identityKey == contact.identityKey }) else { return }
        contacts[idx] = Contact(identityKey: contact.identityKey,
                                encryptionKey: contact.encryptionKey,
                                nickname: nickname)
        localStore.saveContacts(contacts)
        Task { try? await apiClient.upsertContact(key: contact.identityKey, nickname: nickname) }
    }

    // MARK: - Server contact sync

    private func syncContactsFromServer() async {
        guard let serverContacts = try? await apiClient.getContacts() else { return }
        for sc in serverContacts {
            if let idx = contacts.firstIndex(where: { $0.identityKey == sc.contactKey }) {
                // Update nickname if changed on server.
                if contacts[idx].nickname != sc.nickname {
                    contacts[idx] = Contact(identityKey: sc.contactKey,
                                            encryptionKey: contacts[idx].encryptionKey,
                                            nickname: sc.nickname)
                }
            } else {
                // New contact from server — fetch encryption key then add.
                if let user = try? await apiClient.getUser(identityKey: sc.contactKey) {
                    let contact = Contact(identityKey: sc.contactKey,
                                         encryptionKey: user.encryptionKey,
                                         nickname: sc.nickname)
                    contacts.append(contact)
                    if conversations[sc.contactKey] == nil {
                        conversations[sc.contactKey] = localStore.loadMessages(forKey: sc.contactKey)
                    }
                }
            }
        }
        localStore.saveContacts(contacts)
    }

    // MARK: - Send message

    func sendMessage(to contact: Contact, text: String) async throws {
        let (ephemeralKey, ciphertext) = try cryptoEngine.encrypt(
            text: text,
            recipientEncryptionKey: contact.encryptionKey
        )
        let (senderEphemeralKey, senderCiphertext) = try cryptoEngine.encryptForSelf(text: text)

        let serverResponse = try await apiClient.sendMessage(
            recipientKey: contact.identityKey,
            ephemeralKey: ephemeralKey,
            ciphertext: ciphertext,
            senderEphemeralKey: senderEphemeralKey,
            senderCiphertext: senderCiphertext
        )

        let stored = StoredMessage(
            id: serverResponse.id,
            senderKey: keyManager.identityKeyString,
            text: text,
            createdAt: serverResponse.createdAt,
            isOutgoing: true
        )
        appendToConversation(stored, forKey: contact.identityKey)
    }

    // MARK: - WebSocket

    private func connectWebSocket() {
        guard let authHeader = try? keyManager.authHeader() else { return }
        wsClient.onMessage = { [weak self] response in
            Task { @MainActor [weak self] in
                self?.handleIncoming(response)
            }
        }
        wsClient.connect(baseURL: serverBaseURL, authHeader: authHeader)
    }

    private func handleIncoming(_ response: MessageResponse) {
        let senderKey = response.senderKey
        let isEcho = senderKey == keyManager.identityKeyString

        if isEcho {
            // This is an echo of a message we sent from another device.
            // Decrypt using the sender copy and place it in the recipient's conversation.
            guard let recipientKey = response.recipientKey,
                  let sek = response.senderEphemeralKey,
                  let sct = response.senderCiphertext else { return }
            let text: String
            do {
                text = try cryptoEngine.decrypt(ephemeralKey: sek, ciphertext: sct)
            } catch {
                print("AppState: echo decryption failed for message \(response.id): \(error)")
                return
            }
            let stored = StoredMessage(
                id: response.id,
                senderKey: senderKey,
                text: text,
                createdAt: response.createdAt,
                isOutgoing: true
            )
            appendToConversation(stored, forKey: recipientKey)
            return
        }

        // Incoming message from another user.
        let text: String
        do {
            text = try cryptoEngine.decrypt(ephemeralKey: response.ephemeralKey, ciphertext: response.ciphertext)
        } catch {
            print("AppState: decryption failed for message \(response.id): \(error)")
            return
        }

        // Use existing contact nickname, or auto-create an unknown contact.
        let senderName: String
        if let contact = contacts.first(where: { $0.identityKey == senderKey }) {
            senderName = contact.nickname
        } else {
            let nick = String(senderKey.prefix(8)) + "…"
            let unknown = Contact(identityKey: senderKey, encryptionKey: "", nickname: nick)
            addContact(unknown)
            senderName = nick
        }

        let stored = StoredMessage(
            id: response.id,
            senderKey: senderKey,
            text: text,
            createdAt: response.createdAt,
            isOutgoing: false
        )
        appendToConversation(stored, forKey: senderKey)
        if activeChatKey != senderKey {
            scheduleLocalNotification(from: senderName, text: text, id: response.id, senderKey: senderKey)
        }
    }

    private func scheduleLocalNotification(from sender: String, text: String, id: String, senderKey: String) {
        let content = UNMutableNotificationContent()
        content.title = sender
        content.body = text
        content.sound = .default
        content.userInfo = ["sender_key": senderKey]
        let request = UNNotificationRequest(
            identifier: id,
            content: content,
            trigger: nil
        )
        UNUserNotificationCenter.current().add(request)
    }

    // MARK: - History fetch

    private func fetchHistory() async {
        // Incremental sync: only fetch messages newer than the newest local message.
        let since = localStore.newestMessageDate()
        guard let messages = try? await apiClient.getHistory(limit: 100, since: since, beforeID: nil) else { return }
        processHistoryResponses(messages)
    }

    /// Loads the next older page of messages for a contact and prepends them to the conversation.
    func loadOlderMessages(contact: Contact) async {
        let oldestID = localStore.oldestMessageID(forKey: contact.identityKey)
        let limit = 50
        guard let messages = try? await apiClient.getHistory(limit: limit, since: nil, beforeID: oldestID) else { return }
        // Server returns DESC order for before_id; reverse to get ASC for prepending.
        let reversed = messages.reversed()
        processHistoryResponses(Array(reversed), prepending: true, forContactKey: contact.identityKey)
        // If fewer messages than requested were returned, there are no more older messages.
        if messages.count < limit {
            hasMoreHistory[contact.identityKey] = false
        }
    }

    /// Decrypts and stores a batch of history responses.
    /// When prepending is true, new messages are inserted before existing ones in the conversation.
    private func processHistoryResponses(_ messages: [MessageResponse], prepending: Bool = false, forContactKey: String? = nil) {
        let myKey = keyManager.identityKeyString
        for response in messages {
            let isOutgoing = response.senderKey == myKey
            guard let contactKey = isOutgoing ? response.recipientKey : response.senderKey else { continue }
            // If we're doing a targeted prepend, skip messages for other contacts.
            if let target = forContactKey, contactKey != target { continue }

            let text: String
            if isOutgoing {
                guard let sek = response.senderEphemeralKey, let sct = response.senderCiphertext else { continue }
                guard let t = try? cryptoEngine.decrypt(ephemeralKey: sek, ciphertext: sct) else { continue }
                text = t
            } else {
                guard let t = try? cryptoEngine.decrypt(ephemeralKey: response.ephemeralKey, ciphertext: response.ciphertext) else { continue }
                text = t
            }

            let stored = StoredMessage(
                id: response.id,
                senderKey: response.senderKey,
                text: text,
                createdAt: response.createdAt,
                isOutgoing: isOutgoing
            )
            if prepending {
                prependToConversation(stored, forKey: contactKey)
            } else {
                appendToConversation(stored, forKey: contactKey)
            }
        }
    }

    // MARK: - Pending messages poll

    private func fetchPendingMessages() async {
        guard let messages = try? await apiClient.getPendingMessages() else { return }
        for response in messages {
            handleIncoming(response)
        }
    }

    // MARK: - Helpers

    private func appendToConversation(_ message: StoredMessage, forKey identityKey: String) {
        var thread = conversations[identityKey] ?? []
        guard !thread.contains(where: { $0.id == message.id }) else { return }
        thread.append(message)
        conversations[identityKey] = thread
        localStore.appendMessage(message, forKey: identityKey)
    }

    private func prependToConversation(_ message: StoredMessage, forKey identityKey: String) {
        var thread = conversations[identityKey] ?? []
        guard !thread.contains(where: { $0.id == message.id }) else { return }
        thread.insert(message, at: 0)
        conversations[identityKey] = thread
        localStore.appendMessage(message, forKey: identityKey)
    }
}
