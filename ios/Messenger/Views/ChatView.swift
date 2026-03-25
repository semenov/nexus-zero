import SwiftUI

struct ChatView: View {

    let contact: Contact

    @EnvironmentObject private var appState: AppState
    @State private var inputText: String = ""
    @State private var isSending = false
    @State private var errorMessage: String? = nil

    private var messages: [StoredMessage] {
        appState.conversations[contact.identityKey] ?? []
    }

    var body: some View {
        ZStack {
            Theme.background.ignoresSafeArea()
            VStack(spacing: 0) {
                messagesScrollView
                inputBar
            }
        }
        .navigationBarTitleDisplayMode(.inline)
        .navigationBarBackButtonHidden(true)
        .toolbarBackground(Theme.surface, for: .navigationBar)
        .toolbarBackground(.visible, for: .navigationBar)
        .toolbarColorScheme(.dark, for: .navigationBar)
        .toolbar {
            ToolbarItem(placement: .navigationBarLeading) {
                BackButton()
            }
            ToolbarItem(placement: .principal) {
                VStack(spacing: 1) {
                    Text(contact.nickname.uppercased())
                        .font(Theme.mono(14, weight: .bold))
                        .foregroundStyle(Theme.neonGreen)
                        .neonGlow()
                    Text(String(contact.identityKey.prefix(16)) + "…")
                        .font(Theme.mono(9))
                        .foregroundStyle(Theme.textDim)
                }
            }
        }
        .preferredColorScheme(.dark)
        .onAppear  { appState.activeChatKey = contact.identityKey }
        .onDisappear { appState.activeChatKey = nil }
    }

    // MARK: - Messages scroll view

    @ViewBuilder
    private var messagesScrollView: some View {
        if #available(iOS 17, *) {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 6) {
                        if appState.hasMoreHistory[contact.identityKey] ?? true {
                            Button("Load earlier messages") {
                                Task { await appState.loadOlderMessages(contact: contact) }
                            }
                            .font(Theme.mono(11))
                            .foregroundStyle(Theme.textDim)
                            .padding(.vertical, 8)
                        }
                        ForEach(messages) { message in
                            MessageBubble(message: message).id(message.id)
                        }
                    }
                    .padding(.horizontal, 12)
                    .padding(.vertical, 10)
                }
                .defaultScrollAnchor(.bottom)
                .onChange(of: messages.count) { _ in scrollToBottom(proxy: proxy, animated: true) }
            }
        } else {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 6) {
                        if appState.hasMoreHistory[contact.identityKey] ?? true {
                            Button("Load earlier messages") {
                                Task { await appState.loadOlderMessages(contact: contact) }
                            }
                            .font(Theme.mono(11))
                            .foregroundStyle(Theme.textDim)
                            .padding(.vertical, 8)
                        }
                        ForEach(messages) { message in
                            MessageBubble(message: message).id(message.id)
                        }
                    }
                    .padding(.horizontal, 12)
                    .padding(.vertical, 10)
                }
                .onAppear {
                    DispatchQueue.main.asyncAfter(deadline: .now() + 0.15) {
                        scrollToBottom(proxy: proxy, animated: false)
                    }
                }
                .onChange(of: messages.count) { _ in scrollToBottom(proxy: proxy, animated: true) }
            }
        }
    }

    // MARK: - Input bar

    private var inputBar: some View {
        VStack(spacing: 0) {
            Divider().background(Theme.border)
            if let err = errorMessage {
                Text(err)
                    .font(Theme.mono(10))
                    .foregroundStyle(Theme.neonMagenta)
                    .padding(.horizontal)
                    .padding(.top, 6)
            }
            HStack(alignment: .bottom, spacing: 10) {
                Text(">")
                    .font(Theme.mono(16, weight: .bold))
                    .foregroundStyle(Theme.neonGreen)
                    .neonGlow()

                TextField("", text: $inputText)
                    .font(Theme.mono(14))
                    .foregroundStyle(Theme.textPrimary)
                    .tint(Theme.neonGreen)
                    .disabled(isSending)
                    .submitLabel(.send)
                    .onSubmit {
                        if canSend { Task { await sendMessage() } }
                    }

                Button {
                    Task { await sendMessage() }
                } label: {
                    Text("SEND")
                        .font(Theme.mono(12, weight: .bold))
                        .foregroundStyle(canSend ? Theme.neonGreen : Theme.textDim)
                        .neonGlow(canSend ? Theme.neonGreen : .clear)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 6)
                        .overlay(
                            RoundedRectangle(cornerRadius: 3)
                                .stroke(canSend ? Theme.neonGreen : Theme.textDim, lineWidth: 1)
                        )
                }
                .disabled(!canSend)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 10)
            .background(Theme.surface)
        }
    }

    // MARK: - Helpers

    private var canSend: Bool {
        !inputText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && !isSending
    }

    private func sendMessage() async {
        let text = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        isSending = true
        errorMessage = nil
        inputText = ""
        do {
            try await appState.sendMessage(to: contact, text: text)
        } catch {
            errorMessage = error.localizedDescription
            inputText = text
        }
        isSending = false
    }

    private func scrollToBottom(proxy: ScrollViewProxy, animated: Bool) {
        guard let last = messages.last else { return }
        if animated {
            withAnimation { proxy.scrollTo(last.id, anchor: .bottom) }
        } else {
            proxy.scrollTo(last.id, anchor: .bottom)
        }
    }
}

// MARK: - BackButton

private struct BackButton: View {
    @Environment(\.dismiss) private var dismiss
    var body: some View {
        Button {
            dismiss()
        } label: {
            Image(systemName: "chevron.left")
                .font(.system(size: 16, weight: .semibold))
                .foregroundStyle(Theme.neonGreen)
        }
    }
}

// MARK: - MessageBubble

private struct MessageBubble: View {
    let message: StoredMessage

    var body: some View {
        HStack(alignment: .bottom, spacing: 0) {
            if message.isOutgoing { Spacer(minLength: 50) }

            VStack(alignment: message.isOutgoing ? .trailing : .leading, spacing: 3) {
                Text(message.text)
                    .font(Theme.mono(13))
                    .foregroundStyle(message.isOutgoing ? Theme.background : Theme.textPrimary)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                    .background(message.isOutgoing ? Theme.neonGreen : Theme.surfaceHigh)
                    .clipShape(RoundedRectangle(cornerRadius: 3))
                    .overlay(
                        RoundedRectangle(cornerRadius: 3)
                            .stroke(message.isOutgoing ? Theme.neonGreen : Theme.border, lineWidth: 1)
                    )
                    .neonGlow(message.isOutgoing ? Theme.neonGreen : .clear, radius: 4)

                Text(message.createdAt.formatted(.dateTime.hour().minute()))
                    .font(Theme.mono(9))
                    .foregroundStyle(Theme.textDim)
                    .padding(.horizontal, 4)
            }

            if !message.isOutgoing { Spacer(minLength: 50) }
        }
    }
}
