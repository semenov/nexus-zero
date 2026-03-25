import SwiftUI
import CoreImage
import CoreImage.CIFilterBuiltins

struct SettingsView: View {

    @EnvironmentObject private var appState: AppState
    @Environment(\.dismiss) private var dismiss
    @State private var showCopiedToast = false
    @State private var revealPrivateKey = false
    @State private var showPrivateKeyCopied = false

    private var identityKey: String {
        appState.keyManager.identityKeyString
    }

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.background.ignoresSafeArea()

                ScrollView {
                    VStack(spacing: 0) {
                        // MARK: Identity section
                        sectionHeader("> IDENTITY")

                        VStack(alignment: .leading, spacing: 16) {
                            Text("> SHARE YOUR IDENTITY KEY WITH OTHERS\n> SO THEY CAN MESSAGE YOU")
                                .font(Theme.mono(11))
                                .foregroundStyle(Theme.textSecondary)
                                .frame(maxWidth: .infinity, alignment: .leading)

                            // QR code with neon green border and glow
                            if let qrImage = generateQRCode(from: identityKey) {
                                Image(uiImage: qrImage)
                                    .interpolation(.none)
                                    .resizable()
                                    .scaledToFit()
                                    .frame(width: 180, height: 180)
                                    .padding(8)
                                    .background(Color.white)
                                    .overlay(
                                        Rectangle()
                                            .strokeBorder(Theme.neonGreen, lineWidth: 2)
                                    )
                                    .neonGlow(Theme.neonGreen, radius: 12)
                                    .frame(maxWidth: .infinity)
                            }

                            // Key text
                            Text(identityKey)
                                .font(Theme.mono(10))
                                .foregroundStyle(Theme.textSecondary)
                                .multilineTextAlignment(.center)
                                .textSelection(.enabled)
                                .frame(maxWidth: .infinity)

                            // Copy button
                            Button {
                                UIPasteboard.general.string = identityKey
                                showCopiedToast = true
                                DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
                                    showCopiedToast = false
                                }
                            } label: {
                                HStack(spacing: 8) {
                                    Image(systemName: "doc.on.doc")
                                    Text("COPY KEY")
                                        .tracking(2)
                                }
                                .font(Theme.mono(12, weight: .medium))
                                .foregroundStyle(Theme.neonCyan)
                                .frame(maxWidth: .infinity)
                                .padding(.vertical, 10)
                                .overlay(
                                    Rectangle()
                                        .strokeBorder(Theme.neonCyan.opacity(0.6), lineWidth: 1)
                                )
                            }
                            .neonGlow(Theme.neonCyan, radius: 6)

                            if showCopiedToast {
                                Text("> COPIED TO CLIPBOARD")
                                    .font(Theme.mono(11))
                                    .foregroundStyle(Theme.neonGreen)
                                    .neonGlow(Theme.neonGreen, radius: 4)
                                    .transition(.opacity)
                            }
                        }
                        .padding(16)
                        .background(Theme.surface)
                        .overlay(
                            Rectangle()
                                .strokeBorder(Theme.border, lineWidth: 1)
                        )
                        .padding(.horizontal)
                        .padding(.bottom, 24)

                        // MARK: Private key backup section
                        sectionHeader("> BACKUP CODE")

                        VStack(alignment: .leading, spacing: 12) {
                            Text("> WARNING: KEEP THIS SECRET. ANYONE WITH THIS KEY CAN IMPERSONATE YOU.")
                                .font(Theme.mono(10))
                                .foregroundStyle(Theme.neonMagenta)

                            if revealPrivateKey {
                                Text(appState.keyManager.backupCode)
                                    .font(Theme.mono(10))
                                    .foregroundStyle(Theme.neonYellow)
                                    .textSelection(.enabled)
                                    .frame(maxWidth: .infinity, alignment: .leading)

                                HStack(spacing: 12) {
                                    Button {
                                        UIPasteboard.general.string = appState.keyManager.backupCode
                                        showPrivateKeyCopied = true
                                        DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
                                            showPrivateKeyCopied = false
                                        }
                                    } label: {
                                        Text(showPrivateKeyCopied ? "> COPIED" : "> COPY KEY")
                                            .font(Theme.mono(11, weight: .bold))
                                            .foregroundStyle(showPrivateKeyCopied ? Theme.neonGreen : Theme.neonYellow)
                                            .neonGlow(showPrivateKeyCopied ? Theme.neonGreen : Theme.neonYellow, radius: 4)
                                    }

                                    Spacer()

                                    Button {
                                        revealPrivateKey = false
                                    } label: {
                                        Text("> HIDE")
                                            .font(Theme.mono(11, weight: .bold))
                                            .foregroundStyle(Theme.textSecondary)
                                    }
                                }
                            } else {
                                Button {
                                    revealPrivateKey = true
                                } label: {
                                    Text("> TAP TO REVEAL")
                                        .font(Theme.mono(11, weight: .bold))
                                        .foregroundStyle(Theme.neonMagenta)
                                        .neonGlow(Theme.neonMagenta, radius: 4)
                                }
                            }
                        }
                        .padding()
                        .background(Theme.surface)
                        .overlay(Rectangle().strokeBorder(Theme.neonMagenta.opacity(0.4), lineWidth: 1))
                        .padding(.horizontal)
                        .padding(.bottom, 24)

                        // MARK: About section
                        sectionHeader("> SYSTEM INFO")

                        VStack(spacing: 0) {
                            terminalRow(label: "VERSION", value: "1.0.0")
                            Divider().background(Theme.border)
                            terminalRow(label: "ENCRYPTION", value: "X25519 + CHACHAPOLY")
                            Divider().background(Theme.border)
                            terminalRow(label: "IDENTITY", value: "ED25519")
                        }
                        .background(Theme.surface)
                        .overlay(
                            Rectangle()
                                .strokeBorder(Theme.border, lineWidth: 1)
                        )
                        .padding(.horizontal)
                    }
                    .padding(.top, 8)
                }
            }
            .navigationTitle("")
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(Theme.surface, for: .navigationBar)
            .toolbarBackground(.visible, for: .navigationBar)
            .toolbarColorScheme(.dark, for: .navigationBar)
            .toolbar {
                ToolbarItem(placement: .principal) {
                    Text("// SETTINGS")
                        .font(Theme.mono(16, weight: .bold))
                        .foregroundStyle(Theme.neonGreen)
                        .neonGlow()
                }
                ToolbarItem(placement: .navigationBarTrailing) {
                    Button {
                        dismiss()
                    } label: {
                        Text("✕")
                            .font(Theme.mono(20, weight: .bold))
                            .foregroundStyle(Theme.textSecondary)
                    }
                }
            }
            .preferredColorScheme(.dark)
            .animation(.easeInOut, value: showCopiedToast)
        }
    }

    // MARK: - Subviews

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

    private func terminalRow(label: String, value: String) -> some View {
        HStack {
            Text(label)
                .font(Theme.mono(12))
                .foregroundStyle(Theme.textDim)
                .tracking(1)
            Spacer()
            Text(value)
                .font(Theme.mono(12, weight: .medium))
                .foregroundStyle(Theme.neonCyan)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
    }

    // MARK: - QR generation

    private func generateQRCode(from string: String) -> UIImage? {
        let context = CIContext()
        let filter = CIFilter.qrCodeGenerator()
        filter.message = Data(string.utf8)
        filter.correctionLevel = "M"
        guard let output = filter.outputImage else { return nil }
        let scaled = output.transformed(by: CGAffineTransform(scaleX: 10, y: 10))
        guard let cgImage = context.createCGImage(scaled, from: scaled.extent) else { return nil }
        return UIImage(cgImage: cgImage)
    }
}
