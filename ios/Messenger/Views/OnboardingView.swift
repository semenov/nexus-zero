import SwiftUI
import CoreImage
import CoreImage.CIFilterBuiltins

struct OnboardingView: View {

    @EnvironmentObject private var appState: AppState

    private enum Step { case choice, create, restore }

    @State private var step: Step = .choice
    @State private var isWorking = false
    @State private var generatedKey: String? = nil
    @State private var backupCode: String = ""
    @State private var errorMessage: String? = nil
    @State private var showCopiedToast = false

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.background.ignoresSafeArea()
                VStack(spacing: 32) {
                    Spacer()
                    brandingSection
                    Spacer()
                    switch step {
                    case .choice:  choiceSection
                    case .create:  createSection
                    case .restore: restoreSection
                    }
                    Spacer()
                }
            }
            .navigationBarHidden(true)
            .preferredColorScheme(.dark)
            .animation(.easeInOut, value: step)
            .animation(.easeInOut, value: generatedKey)
            .animation(.easeInOut, value: showCopiedToast)
        }
    }

    // MARK: - Branding

    private var brandingSection: some View {
        VStack(spacing: 16) {
            Image(systemName: "lock.shield.fill")
                .font(.system(size: 64))
                .foregroundStyle(Theme.neonGreen)
                .neonGlow(Theme.neonGreen, radius: 16)

            Text("NEXUS ZERO")
                .font(Theme.mono(26, weight: .bold))
                .foregroundStyle(Theme.neonGreen)
                .neonGlow(Theme.neonGreen, radius: 8)
                .tracking(6)

            Rectangle()
                .fill(Theme.border)
                .frame(height: 1)
                .padding(.horizontal, 40)

            VStack(alignment: .leading, spacing: 4) {
                Text("> NO ACCOUNTS. NO PASSWORDS.")
                Text("> YOUR IDENTITY IS A CRYPTOGRAPHIC KEY.")
                Text("> NOBODY CAN TAKE IT FROM YOU.")
            }
            .font(Theme.mono(11))
            .foregroundStyle(Theme.textSecondary)
            .padding(.horizontal, 32)
        }
    }

    // MARK: - Choice screen

    private var choiceSection: some View {
        VStack(spacing: 12) {
            cyberButton("[ CREATE NEW IDENTITY ]", color: Theme.neonGreen) {
                step = .create
            }

            cyberButton("[ RESTORE FROM BACKUP ]", color: Theme.neonCyan) {
                step = .restore
            }
        }
        .padding(.horizontal)
    }

    // MARK: - Create screen

    private var createSection: some View {
        VStack(spacing: 16) {
            if let key = generatedKey {
                // Show public identity key + QR
                VStack(spacing: 16) {
                    HStack {
                        Text("> YOUR IDENTITY KEY")
                            .font(Theme.mono(11, weight: .bold))
                            .foregroundStyle(Theme.textSecondary)
                        Spacer()
                    }
                    .padding(.horizontal, 4)

                    if let qrImage = generateQRCode(from: key) {
                        Image(uiImage: qrImage)
                            .interpolation(.none)
                            .resizable()
                            .scaledToFit()
                            .frame(width: 180, height: 180)
                            .padding(8)
                            .background(Color.white)
                            .overlay(Rectangle().strokeBorder(Theme.neonGreen, lineWidth: 2))
                            .neonGlow(Theme.neonGreen, radius: 12)
                    }

                    Text(key)
                        .font(Theme.mono(9))
                        .foregroundStyle(Theme.textSecondary)
                        .multilineTextAlignment(.center)
                        .padding(.horizontal)

                    Button {
                        UIPasteboard.general.string = key
                        showCopiedToast = true
                        DispatchQueue.main.asyncAfter(deadline: .now() + 2) { showCopiedToast = false }
                    } label: {
                        Text(showCopiedToast ? "> COPIED" : "> COPY KEY")
                            .font(Theme.mono(11, weight: .bold))
                            .foregroundStyle(Theme.neonCyan)
                            .neonGlow(Theme.neonCyan, radius: 4)
                    }
                }
                .padding(20)
                .background(Theme.surface)
                .overlay(Rectangle().strokeBorder(Theme.border, lineWidth: 1))
                .padding(.horizontal)

                cyberButton("[ INITIALIZE ]", color: Theme.neonGreen) {
                    appState.setup()
                }
                .padding(.horizontal)

            } else {
                errorView
                cyberButton(isWorking ? "GENERATING…" : "[ GENERATE KEYPAIR ]",
                            color: isWorking ? Theme.dimGreen : Theme.neonGreen,
                            disabled: isWorking) {
                    Task { await generateKeys() }
                }
                .padding(.horizontal)

                backButton
            }
        }
    }

    // MARK: - Restore screen

    private var restoreSection: some View {
        VStack(spacing: 16) {
            VStack(alignment: .leading, spacing: 8) {
                Text("> PASTE YOUR BACKUP CODE")
                    .font(Theme.mono(11, weight: .bold))
                    .foregroundStyle(Theme.textSecondary)

                TextEditor(text: $backupCode)
                    .font(Theme.mono(11))
                    .foregroundStyle(Theme.textPrimary)
                    .scrollContentBackground(.hidden)
                    .background(Theme.surface)
                    .frame(height: 80)
                    .overlay(Rectangle().strokeBorder(Theme.border, lineWidth: 1))
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)

                Text("> BACKUP CODE = SIGNING KEY + AGREEMENT KEY")
                    .font(Theme.mono(9))
                    .foregroundStyle(Theme.textDim)
            }
            .padding(.horizontal)

            errorView

            cyberButton(isWorking ? "RESTORING…" : "[ RESTORE ACCOUNT ]",
                        color: isWorking ? Theme.dimCyan : Theme.neonCyan,
                        disabled: isWorking || backupCode.trimmingCharacters(in: .whitespaces).isEmpty) {
                Task { await restoreKeys() }
            }
            .padding(.horizontal)

            backButton
        }
    }

    // MARK: - Shared subviews

    private var errorView: some View {
        Group {
            if let err = errorMessage {
                HStack(spacing: 6) {
                    Text("! ERR: \(err.uppercased())")
                }
                .font(Theme.mono(11))
                .foregroundStyle(Theme.neonMagenta)
                .neonGlow(Theme.neonMagenta, radius: 4)
                .padding(.horizontal)
            }
        }
    }

    private var backButton: some View {
        Button {
            step = .choice
            errorMessage = nil
        } label: {
            Text("< BACK")
                .font(Theme.mono(11))
                .foregroundStyle(Theme.textDim)
        }
    }

    @ViewBuilder
    private func cyberButton(_ label: String, color: Color, disabled: Bool = false, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(label)
                .font(Theme.mono(14, weight: .bold))
                .tracking(2)
                .foregroundStyle(disabled ? Theme.textDim : Theme.background)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 14)
                .background(disabled ? Theme.surface : color)
        }
        .neonGlow(disabled ? .clear : color, radius: 8)
        .disabled(disabled)
    }

    // MARK: - Actions

    private func generateKeys() async {
        isWorking = true
        errorMessage = nil
        do {
            try appState.keyManager.generateKeys()
            try? await appState.apiClient.registerUser()
            generatedKey = appState.keyManager.identityKeyString
        } catch {
            errorMessage = error.localizedDescription
        }
        isWorking = false
    }

    private func restoreKeys() async {
        isWorking = true
        errorMessage = nil
        do {
            try appState.keyManager.restoreFromBackupCode(backupCode)
            // Register on this server if not already registered.
            try? await appState.apiClient.registerUser()
            appState.setup()
        } catch {
            errorMessage = error.localizedDescription
        }
        isWorking = false
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
