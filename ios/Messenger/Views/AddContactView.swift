import SwiftUI
import AVFoundation

struct AddContactView: View {

    @EnvironmentObject private var appState: AppState
    @Environment(\.dismiss) private var dismiss

    // Step machine
    @State private var step: Step = .input
    @State private var pastedKey: String = ""
    @State private var nickname: String = ""
    @State private var isLoading = false
    @State private var errorMessage: String? = nil
    @State private var showScanner = false

    private enum Step { case input, nickname, done }

    var body: some View {
        NavigationStack {
            Form {
                switch step {
                case .input:
                    inputSection
                case .nickname:
                    nicknameSection
                case .done:
                    EmptyView()
                }

                if let err = errorMessage {
                    Section {
                        Text(err)
                            .foregroundStyle(.red)
                            .font(.caption)
                    }
                }
            }
            .navigationTitle("Add Contact")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
            .sheet(isPresented: $showScanner) {
                QRScannerSheet { scanned in
                    pastedKey = scanned
                    showScanner = false
                }
            }
        }
    }

    // MARK: - Sections

    private var inputSection: some View {
        Group {
            Section(header: Text("Public Key")) {
                TextField("Paste identity key…", text: $pastedKey, axis: .vertical)
                    .font(.system(.body, design: .monospaced))
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                    .lineLimit(3...6)

                Button {
                    showScanner = true
                } label: {
                    Label("Scan QR Code", systemImage: "qrcode.viewfinder")
                }
            }

            Section {
                Button {
                    Task { await lookupUser() }
                } label: {
                    if isLoading {
                        ProgressView()
                    } else {
                        Text("Next")
                            .frame(maxWidth: .infinity)
                    }
                }
                .disabled(pastedKey.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || isLoading)
            }
        }
    }

    private var nicknameSection: some View {
        Group {
            Section(header: Text("Set a Nickname")) {
                TextField("Nickname", text: $nickname)
                    .autocorrectionDisabled()
            }

            Section {
                Button {
                    saveContact()
                } label: {
                    Text("Add Contact")
                        .frame(maxWidth: .infinity)
                }
                .disabled(nickname.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
        }
    }

    // MARK: - Logic

    /// Resolves the identity key via the server and advances to the nickname step.
    @State private var resolvedEncryptionKey: String = ""

    private func lookupUser() async {
        let key = pastedKey.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !key.isEmpty else { return }
        isLoading = true
        errorMessage = nil
        do {
            let user = try await appState.apiClient.getUser(identityKey: key)
            resolvedEncryptionKey = user.encryptionKey
            step = .nickname
        } catch {
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    private func saveContact() {
        let key = pastedKey.trimmingCharacters(in: .whitespacesAndNewlines)
        let nick = nickname.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !key.isEmpty, !nick.isEmpty else { return }
        let contact = Contact(identityKey: key, encryptionKey: resolvedEncryptionKey, nickname: nick)
        appState.addContact(contact)
        dismiss()
    }
}

// MARK: - QR Scanner

struct QRScannerSheet: View {
    let onScan: (String) -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            QRScannerView(onScan: onScan)
                .ignoresSafeArea()
                .navigationTitle("Scan QR Code")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel") { dismiss() }
                    }
                }
        }
    }
}

// MARK: - UIViewRepresentable camera preview

struct QRScannerView: UIViewRepresentable {

    let onScan: (String) -> Void

    func makeUIView(context: Context) -> QRScannerUIView {
        let view = QRScannerUIView()
        view.onScan = onScan
        return view
    }

    func updateUIView(_ uiView: QRScannerUIView, context: Context) {}
}

final class QRScannerUIView: UIView, AVCaptureMetadataOutputObjectsDelegate {

    var onScan: ((String) -> Void)?

    private var captureSession: AVCaptureSession?
    private var previewLayer: AVCaptureVideoPreviewLayer?

    override func didMoveToSuperview() {
        super.didMoveToSuperview()
        setupSession()
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        previewLayer?.frame = bounds
    }

    private func setupSession() {
        let session = AVCaptureSession()
        guard let device = AVCaptureDevice.default(for: .video),
              let input = try? AVCaptureDeviceInput(device: device),
              session.canAddInput(input) else { return }

        session.addInput(input)

        let output = AVCaptureMetadataOutput()
        guard session.canAddOutput(output) else { return }
        session.addOutput(output)
        output.setMetadataObjectsDelegate(self, queue: .main)
        output.metadataObjectTypes = [.qr]

        let preview = AVCaptureVideoPreviewLayer(session: session)
        preview.videoGravity = .resizeAspectFill
        preview.frame = bounds
        layer.addSublayer(preview)
        previewLayer = preview

        captureSession = session
        DispatchQueue.global(qos: .userInitiated).async { session.startRunning() }
    }

    func metadataOutput(_ output: AVCaptureMetadataOutput,
                        didOutput metadataObjects: [AVMetadataObject],
                        from connection: AVCaptureConnection) {
        guard let obj = metadataObjects.first as? AVMetadataMachineReadableCodeObject,
              let value = obj.stringValue else { return }
        captureSession?.stopRunning()
        onScan?(value)
    }
}
