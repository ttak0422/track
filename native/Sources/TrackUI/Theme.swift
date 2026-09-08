import SwiftUI

// TrackTheme — the ten interface tokens of docs/spec/design.md as SwiftUI
// colors, plus the appearance settings that carry them across the app:
//
// - TrackTheme holds one light and one dark palette. Each hex below is the
//   token's value in that scheme (design.md "Tokens", Light/Dark columns);
//   the web declares the same pair through CSS light-dark() in styles.css.
// - ThemeMode is the explicit choice the Settings tab writes, mirroring the
//   web's themeState.ts: the same "track.theme" storage key, and "system" is
//   the neutral default that stores nothing.
// - TrackAppearance is the two independent text-size settings from the web
//   ThemeMenu (reader and preview), expressed as a scale over the native 16pt
//   base. The old ratio key is retained as a compatibility fallback.
//
// None of the palettes is applied as a ViewModifier. The app maps a mode onto
// SwiftUI's own scheme switch with `.preferredColorScheme(...)` and hands the
// font scale down through the environment, so every surface keeps the native
// controls that already follow the system appearance.

// MARK: - Tokens

/// One scheme's reading of the ten tokens of docs/spec/design.md. Property
/// names strip the CSS dashes: `lineStrong`/`lineNode` are `--line-strong`
/// and `--line-node`, `panelSoft` is `--panel-soft`.
public struct TrackTheme: Sendable {
    public let bg: Color
    public let panel: Color
    public let panelSoft: Color
    public let text: Color
    public let muted: Color
    public let faint: Color
    public let line: Color
    public let lineStrong: Color
    public let lineNode: Color
    public let mark: Color

    public init(
        bg: Color,
        panel: Color,
        panelSoft: Color,
        text: Color,
        muted: Color,
        faint: Color,
        line: Color,
        lineStrong: Color,
        lineNode: Color,
        mark: Color
    ) {
        self.bg = bg
        self.panel = panel
        self.panelSoft = panelSoft
        self.text = text
        self.muted = muted
        self.faint = faint
        self.line = line
        self.lineStrong = lineStrong
        self.lineNode = lineNode
        self.mark = mark
    }
}

extension Color {
    /// 0xRRGGBB → sRGB color. SwiftUI has no hex initializer, so the token
    /// table spells each value exactly as design.md does.
    init(hex: UInt32) {
        self.init(
            .sRGB,
            red: Double((hex >> 16) & 0xFF) / 255.0,
            green: Double((hex >> 8) & 0xFF) / 255.0,
            blue: Double(hex & 0xFF) / 255.0,
            opacity: 1.0
        )
    }
}

public extension TrackTheme {
    /// The design.md Light column, verbatim.
    static let light = TrackTheme(
        bg: Color(hex: 0xfbfaf8),
        panel: Color(hex: 0xffffff),
        panelSoft: Color(hex: 0xf3f2ee),
        text: Color(hex: 0x1a1a18),
        muted: Color(hex: 0x5e5d58),
        faint: Color(hex: 0x6f6e68),
        line: Color(hex: 0xe6e4de),
        lineStrong: Color(hex: 0xc7c5bd),
        lineNode: Color(hex: 0x8e8c84),
        mark: Color(hex: 0xc13a1e)
    )

    /// The design.md Dark column, verbatim.
    static let dark = TrackTheme(
        bg: Color(hex: 0x141618),
        panel: Color(hex: 0x191c1e),
        panelSoft: Color(hex: 0x212528),
        text: Color(hex: 0xe9e9e4),
        muted: Color(hex: 0xa2a29b),
        faint: Color(hex: 0x8b8b83),
        line: Color(hex: 0x282c2f),
        lineStrong: Color(hex: 0x3e4347),
        lineNode: Color(hex: 0x6e7478),
        mark: Color(hex: 0xf4785e)
    )

    /// The palette matching the scheme a view is currently drawn in. Views
    /// read their own `@Environment(\.colorScheme)` and ask here — there is
    /// no cached environment palette, because the system scheme can change
    /// under the app (design.md: light and dark are the same design).
    static func palette(for scheme: ColorScheme) -> TrackTheme {
        scheme == .dark ? .dark : .light
    }
}

// MARK: - Theme mode

/// The explicit theme choice. "system" is the neutral default; only light and
/// dark are ever stored, exactly like web themeState.ts's applyTheme.
public enum ThemeMode: String, CaseIterable, Sendable {
    case system
    case light
    case dark

    /// Reads what themeState.ts's applyTheme stores: nothing (or anything
    /// else) means the system setting decides; only "light"/"dark" are real.
    public init(stored raw: String?) {
        switch raw {
        case "light": self = .light
        case "dark": self = .dark
        default: self = .system
        }
    }

    /// What applyTheme writes back: "system" removes the stored value so the
    /// OS appearance wins (themeState.ts deletes the key for system).
    public var storedValue: String? {
        switch self {
        case .system: return nil
        case .light: return "light"
        case .dark: return "dark"
        }
    }

    /// The SwiftUI scheme to request; nil leaves the OS in charge (the
    /// `.preferredColorScheme(nil)` form of applying "system").
    public var preferredColorScheme: ColorScheme? {
        switch self {
        case .system: return nil
        case .light: return .light
        case .dark: return .dark
        }
    }

    /// Menu label for the Settings picker.
    public var label: String {
        switch self {
        case .system: return "System"
        case .light: return "Light"
        case .dark: return "Dark"
        }
    }
}

// MARK: - Content width

/// The reading-column width, shared with the web ThemeMenu's Normal/Wide/Full
/// choices. Normal is the neutral default and therefore stores nothing.
public enum ContentWidthMode: String, CaseIterable, Sendable {
    case normal
    case wide
    case full

    public init(stored raw: String?) {
        switch raw {
        case "wide": self = .wide
        case "full": self = .full
        default: self = .normal
        }
    }

    public var storedValue: String? {
        self == .normal ? nil : rawValue
    }

    /// Maximum reading-column width in points. Full intentionally delegates
    /// sizing to SwiftUI's available width.
    public var maxWidth: CGFloat {
        switch self {
        case .normal: return 880
        case .wide: return 1280
        case .full: return .infinity
        }
    }

    public var label: String {
        switch self {
        case .normal: return "Normal"
        case .wide: return "Wide"
        case .full: return "Full"
        }
    }
}

// MARK: - Appearance settings

/// Storage keys and the range/step rules for the appearance settings. Keys
/// match the web where one exists (themeState.ts writes "track.theme").
public enum TrackAppearance {
    /// Shared with the web's themeState.ts.
    public static let themeKey = "track.theme"
    /// Web-compatible absolute size settings. Native views consume their
    /// corresponding values as a scale over `baseFontSize`.
    public static let fontSizeKey = "track.fontSize"
    public static let previewFontSizeKey = "track.previewFontSize"
    public static let baseFontSize = 16.0
    public static let fontSizeRange: ClosedRange<Double> = 13...32
    /// Legacy native ratio key. Keep reading it so existing preferences do not
    /// silently reset when the web-compatible settings are introduced.
    public static let fontScaleKey = "track.fontScale"
    public static let defaultFontScale = 1.0
    /// Native reading-column width, matching web ThemeMenu's setting.
    public static let contentWidthKey = "track.contentWidth"
    /// The range the stored Double is held to (0.85–1.3×).
    public static let fontScaleRange: ClosedRange<Double> = 0.85...1.3

    public static func scale(forFontSize size: Double) -> Double {
        min(max(size / baseFontSize, fontSizeRange.lowerBound / baseFontSize), fontSizeRange.upperBound / baseFontSize)
    }

    /// Holds an out-of-range stored value (from an older version or a hand
    /// edit) to the documented range before it is applied.
    public static func clampFontScale(_ scale: Double) -> Double {
        min(max(scale, fontScaleRange.lowerBound), fontScaleRange.upperBound)
    }
}

// MARK: - Environment

private struct TrackFontScaleKey: EnvironmentKey {
    static let defaultValue = TrackAppearance.defaultFontScale
}

private struct TrackPreviewFontScaleKey: EnvironmentKey {
    static let defaultValue = TrackAppearance.scale(forFontSize: TrackAppearance.baseFontSize)
}

public extension EnvironmentValues {
    /// The font scale the app owner injected at the root. Views apply it the
    /// way design.md sizes chrome: base size × scale.
    var trackFontScale: Double {
        get { self[TrackFontScaleKey.self] }
        set { self[TrackFontScaleKey.self] = newValue }
    }

    /// Scale used by note preview surfaces, independent from the main reader.
    var trackPreviewFontScale: Double {
        get { self[TrackPreviewFontScaleKey.self] }
        set { self[TrackPreviewFontScaleKey.self] = newValue }
    }
}
