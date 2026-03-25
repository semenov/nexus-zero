import Foundation
import CryptoKit
import Security

/// Errors that can occur during key-management operations.
enum KeyManagerError: LocalizedError {
    case keychainWrite(OSStatus)
    case keychainRead(OSStatus)
    case keychainDelete(OSStatus)
    case keyNotFound
    case invalidKeyData

    var errorDescription: String? {
        switch self {
        case .keychainWrite(let s):  return "Keychain write failed: \(s)"
        case .keychainRead(let s):   return "Keychain read failed: \(s)"
        case .keychainDelete(let s): return "Keychain delete failed: \(s)"
        case .keyNotFound:           return "Key not found in Keychain"
        case .invalidKeyData:        return "Stored key data is invalid"
        }
    }
}

/// Manages the device's long-term identity and agreement keypairs.
///
/// Keys are stored in the iOS Keychain as `kSecClassGenericPassword` items
/// under service `"messenger"` with distinct accounts per key.
final class KeyManager {

    // MARK: - Keychain constants

    private static let service = "messenger"
    private static let signingAccount = "signingKey"
    private static let agreementAccount = "agreementKey"

    // MARK: - Cached in-memory keys (loaded lazily)

    private var _signingKey: Curve25519.Signing.PrivateKey?
    private var _agreementKey: Curve25519.KeyAgreement.PrivateKey?

    // MARK: - Public accessors

    /// Returns the stored Ed25519 signing private key, or `nil` if keys have
    /// not yet been generated on this device.
    var signingKey: Curve25519.Signing.PrivateKey? {
        if let cached = _signingKey { return cached }
        _signingKey = try? loadKey(account: Self.signingAccount) {
            try Curve25519.Signing.PrivateKey(rawRepresentation: $0)
        }
        return _signingKey
    }

    /// Returns the stored X25519 key-agreement private key, or `nil` if keys
    /// have not yet been generated on this device.
    var agreementKey: Curve25519.KeyAgreement.PrivateKey? {
        if let cached = _agreementKey { return cached }
        _agreementKey = try? loadKey(account: Self.agreementAccount) {
            try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: $0)
        }
        return _agreementKey
    }

    // MARK: - Key generation

    /// Generates new Ed25519 and X25519 keypairs and persists them in the
    /// Keychain, replacing any existing keys.
    func generateKeys() throws {
        let signing = Curve25519.Signing.PrivateKey()
        let agreement = Curve25519.KeyAgreement.PrivateKey()

        try storeKey(data: signing.rawRepresentation, account: Self.signingAccount)
        try storeKey(data: agreement.rawRepresentation, account: Self.agreementAccount)

        _signingKey = signing
        _agreementKey = agreement
    }

    // MARK: - Derived strings

    /// Base64url-encoded (no padding) Ed25519 public key — the user's identity.
    var identityKeyString: String {
        guard let key = signingKey else { return "" }
        return key.publicKey.rawRepresentation.base64URLEncodedString()
    }

    /// Backup code combining both private keys: "<signingPriv>.<agreementPriv>"
    /// This single string is sufficient to fully restore the account.
    var backupCode: String {
        guard let sig = signingKey, let agr = agreementKey else { return "" }
        return sig.rawRepresentation.base64URLEncodedString()
            + "." + agr.rawRepresentation.base64URLEncodedString()
    }

    /// Restores keypairs from a backup code produced by `backupCode`.
    /// Throws if the code is malformed or the key data is invalid.
    func restoreFromBackupCode(_ code: String) throws {
        let parts = code.trimmingCharacters(in: .whitespaces).components(separatedBy: ".")
        guard parts.count == 2,
              let sigData = Data(base64URLEncoded: parts[0]),
              let agrData = Data(base64URLEncoded: parts[1]) else {
            throw KeyManagerError.invalidKeyData
        }
        let signing   = try Curve25519.Signing.PrivateKey(rawRepresentation: sigData)
        let agreement = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: agrData)
        try storeKey(data: signing.rawRepresentation,   account: Self.signingAccount)
        try storeKey(data: agreement.rawRepresentation, account: Self.agreementAccount)
        _signingKey   = signing
        _agreementKey = agreement
    }

    /// Base64url-encoded (no padding) X25519 public key — used for encryption.
    var encryptionKeyString: String {
        guard let key = agreementKey else { return "" }
        return key.publicKey.rawRepresentation.base64URLEncodedString()
    }

    // MARK: - Auth header

    /// Builds the `Ed25519 <pubkey>.<timestamp>.<signature>` authorization
    /// header value.
    func authHeader() throws -> String {
        guard let signing = signingKey else { throw KeyManagerError.keyNotFound }
        let pubB64 = identityKeyString
        let ts = String(Int(Date().timeIntervalSince1970))
        let message = "\(pubB64).\(ts)"
        let sig = try signing.signature(for: Data(message.utf8))
        let sigB64 = sig.base64URLEncodedString()
        return "Ed25519 \(pubB64).\(ts).\(sigB64)"
    }

    // MARK: - Keychain helpers

    private func storeKey(data: Data, account: String) throws {
        // Delete any existing item first.
        let deleteQuery: [CFString: Any] = [
            kSecClass:   kSecClassGenericPassword,
            kSecAttrService: Self.service,
            kSecAttrAccount: account,
        ]
        SecItemDelete(deleteQuery as CFDictionary)

        let addQuery: [CFString: Any] = [
            kSecClass:           kSecClassGenericPassword,
            kSecAttrService:     Self.service,
            kSecAttrAccount:     account,
            kSecValueData:       data,
            kSecAttrAccessible:  kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
        ]
        let status = SecItemAdd(addQuery as CFDictionary, nil)
        guard status == errSecSuccess else { throw KeyManagerError.keychainWrite(status) }
    }

    private func loadKey<K>(account: String, parse: (Data) throws -> K) throws -> K {
        let query: [CFString: Any] = [
            kSecClass:            kSecClassGenericPassword,
            kSecAttrService:      Self.service,
            kSecAttrAccount:      account,
            kSecReturnData:       true,
            kSecMatchLimit:       kSecMatchLimitOne,
        ]
        var result: AnyObject?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        guard status == errSecSuccess else {
            if status == errSecItemNotFound { throw KeyManagerError.keyNotFound }
            throw KeyManagerError.keychainRead(status)
        }
        guard let data = result as? Data else { throw KeyManagerError.invalidKeyData }
        return try parse(data)
    }
}

