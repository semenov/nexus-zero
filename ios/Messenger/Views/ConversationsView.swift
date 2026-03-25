import SwiftUI

struct ConversationsView: View {

    @EnvironmentObject private var appState: AppState
    @State private var showAddContact = false
    @State private var showSettings = false
    @State private var renameTarget: Contact? = nil
    @State private var renameText: String = ""
    @State private var navPath = NavigationPath()

    var body: some View {
        NavigationStack(path: $navPath) {
            ZStack {
                Theme.background.ignoresSafeArea()

                Group {
                    if appState.contacts.isEmpty {
                        emptyState
                    } else {
                        contactList
                    }
                }
            }
            .navigationTitle("// MESSAGES")
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(Theme.surface, for: .navigationBar)
            .toolbarBackground(.visible, for: .navigationBar)
            .toolbarColorScheme(.dark, for: .navigationBar)
            .toolbar {
                ToolbarItem(placement: .navigationBarLeading) {
                    Button {
                        showSettings = true
                    } label: {
                        Image(systemName: "terminal")
                            .foregroundStyle(Theme.textSecondary)
                    }
                }
                ToolbarItem(placement: .principal) {
                    Text("// MESSAGES")
                        .font(Theme.mono(16, weight: .bold))
                        .foregroundStyle(Theme.neonGreen)
                        .neonGlow()
                }
                ToolbarItem(placement: .navigationBarTrailing) {
                    Button {
                        showAddContact = true
                    } label: {
                        Image(systemName: "plus")
                            .foregroundStyle(Theme.neonGreen)
                            .neonGlow()
                    }
                }
            }
            .sheet(isPresented: $showAddContact) {
                AddContactView()
            }
            .sheet(isPresented: $showSettings) {
                SettingsView()
            }
            .alert("// RENAME", isPresented: Binding(
                get: { renameTarget != nil },
                set: { if !$0 { renameTarget = nil } }
            )) {
                TextField("nickname", text: $renameText)
                    .autocorrectionDisabled()
                Button("SAVE") {
                    if let contact = renameTarget,
                       !renameText.trimmingCharacters(in: .whitespaces).isEmpty {
                        appState.renameContact(contact, to: renameText.trimmingCharacters(in: .whitespaces))
                    }
                    renameTarget = nil
                }
                Button("CANCEL", role: .cancel) { renameTarget = nil }
            } message: {
                if let c = renameTarget { Text(c.nickname) }
            }
            .onChange(of: renameTarget) { target in
                renameText = target?.nickname ?? ""
            }
            .onChange(of: appState.pendingOpenContactKey) { key in
                guard let key,
                      let contact = appState.contacts.first(where: { $0.identityKey == key }) else { return }
                navPath.removeLast(navPath.count)
                navPath.append(contact)
                appState.pendingOpenContactKey = nil
            }
            .navigationDestination(for: Contact.self) { contact in
                ChatView(contact: contact)
            }
        }
        .preferredColorScheme(.dark)
    }

    // MARK: - Subviews

    private var emptyState: some View {
        VStack(spacing: 20) {
            Text(">_")
                .font(Theme.mono(48, weight: .bold))
                .foregroundStyle(Theme.neonGreen)
                .neonGlow()
            Text("NO ACTIVE CHANNELS")
                .font(Theme.mono(14, weight: .bold))
                .foregroundStyle(Theme.textSecondary)
            Text("tap + to add a contact")
                .font(Theme.mono(12))
                .foregroundStyle(Theme.textDim)
        }
    }

    private var contactList: some View {
        ScrollView {
            LazyVStack(spacing: 1) {
                ForEach(appState.contacts) { contact in
                    NavigationLink(value: contact) {
                        ContactRow(contact: contact,
                                   lastMessage: appState.conversations[contact.identityKey]?.last)
                    }
                    .buttonStyle(.plain)
                    .contextMenu {
                        Button {
                            renameTarget = contact
                        } label: {
                            Label("Rename", systemImage: "pencil")
                        }
                    }
                    Divider().background(Theme.border)
                }
            }
        }
    }
}

// MARK: - ContactRow

private struct ContactRow: View {
    let contact: Contact
    let lastMessage: StoredMessage?

    var body: some View {
        HStack(spacing: 14) {
            // Avatar
            ZStack {
                RoundedRectangle(cornerRadius: 4)
                    .fill(Theme.surface)
                    .frame(width: 44, height: 44)
                    .overlay(
                        RoundedRectangle(cornerRadius: 4)
                            .stroke(avatarColor(for: contact.identityKey), lineWidth: 1)
                    )
                Text(contact.nickname.prefix(2).uppercased())
                    .font(Theme.mono(14, weight: .bold))
                    .foregroundStyle(avatarColor(for: contact.identityKey))
                    .neonGlow(avatarColor(for: contact.identityKey), radius: 4)
            }

            VStack(alignment: .leading, spacing: 4) {
                Text(contact.nickname.uppercased())
                    .font(Theme.mono(13, weight: .bold))
                    .foregroundStyle(Theme.neonGreen)

                if let msg = lastMessage {
                    Text(msg.isOutgoing ? "> \(msg.text)" : "< \(msg.text)")
                        .font(Theme.mono(11))
                        .foregroundStyle(Theme.textSecondary)
                        .lineLimit(1)
                } else {
                    Text("no transmissions")
                        .font(Theme.mono(11))
                        .foregroundStyle(Theme.textDim)
                }
            }

            Spacer()

            if let msg = lastMessage {
                Text(msg.createdAt.formatted(.dateTime.hour().minute()))
                    .font(Theme.mono(10))
                    .foregroundStyle(Theme.textDim)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .background(Theme.background)
    }

    private func avatarColor(for key: String) -> Color {
        let colors: [Color] = [Theme.neonGreen, Theme.neonCyan, Theme.neonMagenta, Theme.neonYellow]
        return colors[abs(key.hashValue) % colors.count]
    }
}
