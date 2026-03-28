import SwiftUI

struct ConversationsView: View {

    @EnvironmentObject private var appState: AppState
    @State private var showCreateNexus = false
    @State private var showJoinNexus = false
    @State private var showSettings = false
    @State private var navPath = NavigationPath()

    var body: some View {
        NavigationStack(path: $navPath) {
            ZStack {
                Theme.background.ignoresSafeArea()

                Group {
                    if appState.nexuses.isEmpty {
                        emptyState
                    } else {
                        nexusList
                    }
                }
            }
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
                    Text("// NEXUSES")
                        .font(Theme.mono(16, weight: .bold))
                        .foregroundStyle(Theme.neonGreen)
                        .neonGlow()
                }
                ToolbarItemGroup(placement: .navigationBarTrailing) {
                    Button {
                        showJoinNexus = true
                    } label: {
                        Image(systemName: "key.fill")
                            .foregroundStyle(Theme.neonCyan)
                            .neonGlow(Theme.neonCyan)
                    }
                    Button {
                        showCreateNexus = true
                    } label: {
                        Image(systemName: "plus")
                            .foregroundStyle(Theme.neonGreen)
                            .neonGlow()
                    }
                }
            }
            .sheet(isPresented: $showCreateNexus) {
                CreateNexusView()
            }
            .sheet(isPresented: $showJoinNexus) {
                JoinNexusView()
            }
            .sheet(isPresented: $showSettings) {
                SettingsView()
            }
            .onChange(of: appState.pendingOpenNexusId) { nexusId in
                guard let nexusId,
                      let nexus = appState.nexuses.first(where: { $0.id == nexusId }) else { return }
                navPath.removeLast(navPath.count)
                navPath.append(nexus)
                appState.pendingOpenNexusId = nil
            }
            .navigationDestination(for: Nexus.self) { nexus in
                ChatView(nexus: nexus)
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
            Text("NO NEXUSES")
                .font(Theme.mono(14, weight: .bold))
                .foregroundStyle(Theme.textSecondary)
            Text("tap + to create or J to join")
                .font(Theme.mono(12))
                .foregroundStyle(Theme.textDim)
        }
    }

    private var nexusList: some View {
        ScrollView {
            LazyVStack(spacing: 1) {
                ForEach(appState.nexuses) { nexus in
                    NavigationLink(value: nexus) {
                        NexusRow(nexus: nexus,
                                 lastMessage: appState.conversations[nexus.id]?.last)
                    }
                    .buttonStyle(.plain)
                    Divider().background(Theme.border)
                }
            }
        }
    }
}

// MARK: - NexusRow

private struct NexusRow: View {
    let nexus: Nexus
    let lastMessage: StoredMessage?

    var body: some View {
        HStack(spacing: 14) {
            ZStack {
                RoundedRectangle(cornerRadius: 4)
                    .fill(Theme.surface)
                    .frame(width: 44, height: 44)
                    .overlay(
                        RoundedRectangle(cornerRadius: 4)
                            .stroke(avatarColor(for: nexus.id), lineWidth: 1)
                    )
                Text(nexus.name.prefix(2).uppercased())
                    .font(Theme.mono(14, weight: .bold))
                    .foregroundStyle(avatarColor(for: nexus.id))
                    .neonGlow(avatarColor(for: nexus.id), radius: 4)
            }

            VStack(alignment: .leading, spacing: 4) {
                HStack {
                    Text(nexus.name.uppercased())
                        .font(Theme.mono(13, weight: .bold))
                        .foregroundStyle(Theme.neonGreen)
                    Spacer()
                    Text("\(nexus.members.count)m")
                        .font(Theme.mono(10))
                        .foregroundStyle(Theme.textDim)
                }

                if let msg = lastMessage {
                    let sender = msg.isOutgoing ? "you" : (msg.senderUsername ?? String(msg.senderKey.prefix(6)))
                    Text("\(sender): \(msg.text)")
                        .font(Theme.mono(11))
                        .foregroundStyle(Theme.textSecondary)
                        .lineLimit(1)
                } else {
                    Text("no transmissions")
                        .font(Theme.mono(11))
                        .foregroundStyle(Theme.textDim)
                }
            }

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
