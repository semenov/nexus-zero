import SwiftUI

enum Theme {
    // MARK: - Colors
    static let background   = Color(red: 0.04, green: 0.04, blue: 0.07)
    static let surface      = Color(red: 0.07, green: 0.07, blue: 0.12)
    static let surfaceHigh  = Color(red: 0.10, green: 0.10, blue: 0.17)

    static let neonGreen    = Color(red: 0.00, green: 1.00, blue: 0.25)
    static let neonCyan     = Color(red: 0.00, green: 0.90, blue: 1.00)
    static let neonMagenta  = Color(red: 1.00, green: 0.00, blue: 0.90)
    static let neonYellow   = Color(red: 0.95, green: 1.00, blue: 0.00)

    static let dimGreen     = Color(red: 0.00, green: 0.55, blue: 0.15)
    static let dimCyan      = Color(red: 0.00, green: 0.45, blue: 0.55)

    static let textPrimary  = Color(red: 0.85, green: 1.00, blue: 0.88)
    static let textSecondary = Color(red: 0.30, green: 0.60, blue: 0.35)
    static let textDim      = Color(red: 0.20, green: 0.35, blue: 0.22)

    static let border       = Color(red: 0.00, green: 0.55, blue: 0.20).opacity(0.6)

    // MARK: - Fonts
    static func mono(_ size: CGFloat, weight: Font.Weight = .regular) -> Font {
        .system(size: size, weight: weight, design: .monospaced)
    }

    // MARK: - Reusable modifiers
    static func neonBorder(_ color: Color = Theme.border) -> some ShapeStyle {
        color
    }
}

// MARK: - Neon glow modifier

struct NeonGlow: ViewModifier {
    let color: Color
    let radius: CGFloat

    func body(content: Content) -> some View {
        content
            .shadow(color: color.opacity(0.8), radius: radius * 0.5)
            .shadow(color: color.opacity(0.4), radius: radius)
    }
}

extension View {
    func neonGlow(_ color: Color = Theme.neonGreen, radius: CGFloat = 6) -> some View {
        modifier(NeonGlow(color: color, radius: radius))
    }
}
