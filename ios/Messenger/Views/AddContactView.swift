import SwiftUI

// MARK: - CreateNexusView

struct CreateNexusView: View {

    @EnvironmentObject private var appState: AppState
    @Environment(\.dismiss) private var dismiss

    @State private var name: String = ""
    @State private var isLoading = false
    @State private var errorMessage: String? = nil

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.background.ignoresSafeArea()

                VStack(spacing: 20) {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("> NEXUS NAME")
                            .font(Theme.mono(11, weight: .bold))
                            .foregroundStyle(Theme.textSecondary)

                        TextField("", text: $name)
                            .font(Theme.mono(14))
                            .foregroundStyle(Theme.textPrimary)
                            .tint(Theme.neonGreen)
                            .padding(12)
                            .background(Theme.surface)
                            .overlay(Rectangle().strokeBorder(Theme.border, lineWidth: 1))
                            .autocorrectionDisabled()
                    }
                    .padding(.horizontal)

                    if let err = errorMessage {
                        Text(err)
                            .font(Theme.mono(10))
                            .foregroundStyle(Theme.neonMagenta)
                            .padding(.horizontal)
                    }

                    Button {
                        Task { await create() }
                    } label: {
                        Text(isLoading ? "CREATING…" : "[ CREATE NEXUS ]")
                            .font(Theme.mono(14, weight: .bold))
                            .tracking(2)
                            .foregroundStyle(canCreate ? Theme.background : Theme.textDim)
                            .frame(maxWidth: .infinity)
                            .padding(.vertical, 14)
                            .background(canCreate ? Theme.neonGreen : Theme.surface)
                    }
                    .neonGlow(canCreate ? Theme.neonGreen : .clear, radius: 8)
                    .disabled(!canCreate)
                    .padding(.horizontal)

                    Spacer()
                }
                .padding(.top, 24)
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(Theme.surface, for: .navigationBar)
            .toolbarBackground(.visible, for: .navigationBar)
            .toolbarColorScheme(.dark, for: .navigationBar)
            .toolbar {
                ToolbarItem(placement: .principal) {
                    Text("// CREATE NEXUS")
                        .font(Theme.mono(16, weight: .bold))
                        .foregroundStyle(Theme.neonGreen)
                        .neonGlow()
                }
                ToolbarItem(placement: .cancellationAction) {
                    Button { dismiss() } label: {
                        Text("✕")
                            .font(Theme.mono(20, weight: .bold))
                            .foregroundStyle(Theme.textSecondary)
                    }
                }
            }
            .preferredColorScheme(.dark)
        }
    }

    private var canCreate: Bool {
        !name.trimmingCharacters(in: .whitespaces).isEmpty && !isLoading
    }

    private func create() async {
        isLoading = true
        errorMessage = nil
        do {
            _ = try await appState.createNexus(name: name.trimmingCharacters(in: .whitespaces))
            dismiss()
        } catch {
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }
}

// MARK: - JoinNexusView

struct JoinNexusView: View {

    @EnvironmentObject private var appState: AppState
    @Environment(\.dismiss) private var dismiss

    @State private var code: String = ""
    @State private var isLoading = false
    @State private var errorMessage: String? = nil

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.background.ignoresSafeArea()

                VStack(spacing: 20) {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("> PASTE INVITE CODE")
                            .font(Theme.mono(11, weight: .bold))
                            .foregroundStyle(Theme.textSecondary)

                        TextField("", text: $code)
                            .font(Theme.mono(18, weight: .bold))
                            .foregroundStyle(Theme.neonCyan)
                            .tint(Theme.neonCyan)
                            .multilineTextAlignment(.center)
                            .padding(12)
                            .background(Theme.surface)
                            .overlay(Rectangle().strokeBorder(Theme.neonCyan.opacity(0.5), lineWidth: 1))
                            .autocorrectionDisabled()
                            .textInputAutocapitalization(.characters)
                    }
                    .padding(.horizontal)

                    if let err = errorMessage {
                        Text(err)
                            .font(Theme.mono(10))
                            .foregroundStyle(Theme.neonMagenta)
                            .padding(.horizontal)
                    }

                    Button {
                        Task { await join() }
                    } label: {
                        Text(isLoading ? "JOINING…" : "[ JOIN NEXUS ]")
                            .font(Theme.mono(14, weight: .bold))
                            .tracking(2)
                            .foregroundStyle(canJoin ? Theme.background : Theme.textDim)
                            .frame(maxWidth: .infinity)
                            .padding(.vertical, 14)
                            .background(canJoin ? Theme.neonCyan : Theme.surface)
                    }
                    .neonGlow(canJoin ? Theme.neonCyan : .clear, radius: 8)
                    .disabled(!canJoin)
                    .padding(.horizontal)

                    Spacer()
                }
                .padding(.top, 24)
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(Theme.surface, for: .navigationBar)
            .toolbarBackground(.visible, for: .navigationBar)
            .toolbarColorScheme(.dark, for: .navigationBar)
            .toolbar {
                ToolbarItem(placement: .principal) {
                    Text("// JOIN NEXUS")
                        .font(Theme.mono(16, weight: .bold))
                        .foregroundStyle(Theme.neonCyan)
                        .neonGlow(Theme.neonCyan)
                }
                ToolbarItem(placement: .cancellationAction) {
                    Button { dismiss() } label: {
                        Text("✕")
                            .font(Theme.mono(20, weight: .bold))
                            .foregroundStyle(Theme.textSecondary)
                    }
                }
            }
            .preferredColorScheme(.dark)
        }
    }

    private var canJoin: Bool {
        !code.trimmingCharacters(in: .whitespaces).isEmpty && !isLoading
    }

    private func join() async {
        isLoading = true
        errorMessage = nil
        do {
            _ = try await appState.joinNexus(code: code.trimmingCharacters(in: .whitespaces).uppercased())
            dismiss()
        } catch {
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }
}

// MARK: - NexusSettingsView

struct NexusSettingsView: View {

    let nexus: Nexus

    @EnvironmentObject private var appState: AppState
    @Environment(\.dismiss) private var dismiss

    @State private var inviteCode: String? = nil
    @State private var isGeneratingCode = false
    @State private var showCopiedToast = false
    @State private var errorMessage: String? = nil

    private var currentNexus: Nexus {
        appState.nexuses.first(where: { $0.id == nexus.id }) ?? nexus
    }

    private var isAdmin: Bool {
        currentNexus.role == "admin"
    }

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.background.ignoresSafeArea()

                ScrollView {
                    VStack(spacing: 0) {
                        // Members section
                        sectionHeader("> MEMBERS (\(currentNexus.members.count))")

                        VStack(spacing: 0) {
                            ForEach(currentNexus.members, id: \.identityKey) { member in
                                HStack {
                                    VStack(alignment: .leading, spacing: 2) {
                                        Text(member.username ?? String(member.identityKey.prefix(12)) + "…")
                                            .font(Theme.mono(13, weight: .medium))
                                            .foregroundStyle(Theme.textPrimary)
                                        Text(member.role.uppercased())
                                            .font(Theme.mono(9))
                                            .foregroundStyle(member.role == "admin" ? Theme.neonYellow : Theme.textDim)
                                    }
                                    Spacer()
                                    if isAdmin && member.identityKey != appState.keyManager.identityKeyString {
                                        Button {
                                            Task {
                                                try? await appState.kickMember(nexusId: nexus.id, identityKey: member.identityKey)
                                            }
                                        } label: {
                                            Text("KICK")
                                                .font(Theme.mono(10, weight: .bold))
                                                .foregroundStyle(Theme.neonMagenta)
                                                .padding(.horizontal, 8)
                                                .padding(.vertical, 4)
                                                .overlay(
                                                    RoundedRectangle(cornerRadius: 2)
                                                        .stroke(Theme.neonMagenta.opacity(0.6), lineWidth: 1)
                                                )
                                        }
                                    }
                                }
                                .padding(.horizontal, 16)
                                .padding(.vertical, 10)
                                Divider().background(Theme.border)
                            }
                        }
                        .background(Theme.surface)
                        .overlay(Rectangle().strokeBorder(Theme.border, lineWidth: 1))
                        .padding(.horizontal)
                        .padding(.bottom, 24)

                        // Invite section
                        sectionHeader("> INVITE CODE")

                        VStack(spacing: 12) {
                            if let code = inviteCode {
                                Text(code)
                                    .font(Theme.mono(24, weight: .bold))
                                    .foregroundStyle(Theme.neonCyan)
                                    .neonGlow(Theme.neonCyan, radius: 8)
                                    .textSelection(.enabled)

                                Button {
                                    UIPasteboard.general.string = code
                                    showCopiedToast = true
                                    DispatchQueue.main.asyncAfter(deadline: .now() + 2) { showCopiedToast = false }
                                } label: {
                                    Text(showCopiedToast ? "> COPIED" : "> COPY CODE")
                                        .font(Theme.mono(11, weight: .bold))
                                        .foregroundStyle(Theme.neonCyan)
                                        .neonGlow(Theme.neonCyan, radius: 4)
                                }
                            }

                            Button {
                                Task { await generateInvite() }
                            } label: {
                                Text(isGeneratingCode ? "GENERATING…" : "[ GENERATE NEW CODE ]")
                                    .font(Theme.mono(12, weight: .bold))
                                    .tracking(1)
                                    .foregroundStyle(Theme.background)
                                    .frame(maxWidth: .infinity)
                                    .padding(.vertical, 10)
                                    .background(Theme.neonCyan)
                            }
                            .neonGlow(Theme.neonCyan, radius: 6)
                            .disabled(isGeneratingCode)
                        }
                        .padding(16)
                        .background(Theme.surface)
                        .overlay(Rectangle().strokeBorder(Theme.border, lineWidth: 1))
                        .padding(.horizontal)
                        .padding(.bottom, 24)

                        // Actions section
                        sectionHeader("> ACTIONS")

                        VStack(spacing: 0) {
                            if isAdmin {
                                Button {
                                    Task {
                                        try? await appState.deleteNexus(id: nexus.id)
                                        dismiss()
                                    }
                                } label: {
                                    HStack {
                                        Text("DELETE NEXUS")
                                            .font(Theme.mono(12, weight: .bold))
                                        Spacer()
                                        Image(systemName: "trash")
                                    }
                                    .foregroundStyle(Theme.neonMagenta)
                                    .padding(.horizontal, 16)
                                    .padding(.vertical, 12)
                                }
                            }

                            Button {
                                Task {
                                    try? await appState.leaveNexus(id: nexus.id)
                                    dismiss()
                                }
                            } label: {
                                HStack {
                                    Text("LEAVE NEXUS")
                                        .font(Theme.mono(12, weight: .bold))
                                    Spacer()
                                    Image(systemName: "arrow.right.square")
                                }
                                .foregroundStyle(Theme.neonMagenta)
                                .padding(.horizontal, 16)
                                .padding(.vertical, 12)
                            }
                        }
                        .background(Theme.surface)
                        .overlay(Rectangle().strokeBorder(Theme.neonMagenta.opacity(0.4), lineWidth: 1))
                        .padding(.horizontal)

                        if let err = errorMessage {
                            Text(err)
                                .font(Theme.mono(10))
                                .foregroundStyle(Theme.neonMagenta)
                                .padding()
                        }
                    }
                    .padding(.top, 8)
                }
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(Theme.surface, for: .navigationBar)
            .toolbarBackground(.visible, for: .navigationBar)
            .toolbarColorScheme(.dark, for: .navigationBar)
            .toolbar {
                ToolbarItem(placement: .principal) {
                    Text("// \(currentNexus.name.uppercased())")
                        .font(Theme.mono(16, weight: .bold))
                        .foregroundStyle(Theme.neonGreen)
                        .neonGlow()
                }
                ToolbarItem(placement: .navigationBarTrailing) {
                    Button { dismiss() } label: {
                        Text("✕")
                            .font(Theme.mono(20, weight: .bold))
                            .foregroundStyle(Theme.textSecondary)
                    }
                }
            }
            .preferredColorScheme(.dark)
            .animation(.easeInOut, value: showCopiedToast)
            .onAppear {
                Task { await appState.refreshNexusMembers(nexusId: nexus.id) }
            }
        }
    }

    private func sectionHeader(_ title: String) -> some View {
        HStack {
            Text(title)
                .font(Theme.mono(11, weight: .bold))
                .foregroundStyle(Theme.neonGreen)
                .neonGlow(Theme.neonGreen, radius: 4)
                .tracking(2)
            Spacer()
        }
        .padding(.horizontal, 20)
        .padding(.bottom, 6)
    }

    private func generateInvite() async {
        isGeneratingCode = true
        errorMessage = nil
        do {
            let resp = try await appState.createInvite(nexusId: nexus.id)
            inviteCode = resp.code
        } catch {
            errorMessage = error.localizedDescription
        }
        isGeneratingCode = false
    }
}
