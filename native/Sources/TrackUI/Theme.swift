import AppKit
import SwiftUI

// TrackTheme — the interface tokens of docs/spec/design.md as SwiftUI
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
    /// Visualization-only colors. Unlike `mark`, these are deliberately
    /// available in groups so charts and heatmaps can carry meaning.
    public let danger: Color
    public let chartPalette: [Color]
    public let heatmapRampLo: Color
    public let heatmapRampHi: Color

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
        mark: Color,
        danger: Color = Color(hex: 0x8a352b),
        chartPalette: [Color] = [
            Color(hex: 0x286957), Color(hex: 0xa05f2e), Color(hex: 0x536f91),
            Color(hex: 0x99504a), Color(hex: 0x737b4a), Color(hex: 0x795f80)
        ],
        heatmapRampLo: Color = Color(hex: 0xe4ebe7),
        heatmapRampHi: Color = Color(hex: 0x286957)
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
        self.danger = danger
        self.chartPalette = chartPalette
        self.heatmapRampLo = heatmapRampLo
        self.heatmapRampHi = heatmapRampHi
    }
}

extension Color {
    /// 0xRRGGBB → sRGB color. SwiftUI has no hex initializer, so the token
    /// table spells each value exactly as design.md does.
    public init(hex: UInt32) {
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
        mark: Color(hex: 0xc13a1e),
        danger: Color(hex: 0x8a352b),
        chartPalette: [
            Color(hex: 0x286957), Color(hex: 0xa05f2e), Color(hex: 0x536f91),
            Color(hex: 0x99504a), Color(hex: 0x737b4a), Color(hex: 0x795f80)
        ],
        heatmapRampLo: Color(hex: 0xe4ebe7),
        heatmapRampHi: Color(hex: 0x286957)
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
        mark: Color(hex: 0xf4785e),
        danger: Color(hex: 0xde766b),
        chartPalette: [
            Color(hex: 0x74c4a8), Color(hex: 0xdca06a), Color(hex: 0x9bb7d5),
            Color(hex: 0xdc8b84), Color(hex: 0xb5c383), Color(hex: 0xc1a4c6)
        ],
        heatmapRampLo: Color(hex: 0x28322f),
        heatmapRampHi: Color(hex: 0x91d0b8)
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

// MARK: - Chrome shared recipe (design.md variant 6 + state badges)

///
/// Every section heading (CONTENTS, BACKLINKS, code language, OGP site name,
/// vault name, search groups) wears the same small-caps mono label. Call sites
/// keep only their own margins; the typography lives here so a new label joins
/// the rule instead of restating it.
public extension View {
    /// design.md variant 6: mono 11px, uppercase, faint. The faint ink adapts
    /// to the live scheme at the call site.
    func trackSectionLabel() -> some View {
        modifier(TrackSectionLabelSchemeAware())
    }
}

private struct TrackSectionLabelSchemeAware: ViewModifier {
    @Environment(\.colorScheme) private var scheme
    @Environment(\.trackFontScale) private var scale
    func body(content: Content) -> some View {
        content
            .font(.system(size: 11 * scale, weight: .medium, design: .monospaced))
            .tracking(1.32 * scale)
            .textCase(.uppercase)
            .foregroundStyle(TrackTheme.palette(for: scheme).faint)
    }
}

/// NEW / stale state chip (web `.note-state-badge`): mono 10px in a radius-sm
/// chip. NEW takes the salient mark on a 12% wash; stale is faint on sunk
/// ground. Both are inline state and take no layout of their own.
public struct TrackStateBadge: View {
    public let text: String
    public let kind: Kind
    @Environment(\.colorScheme) private var scheme
    @Environment(\.trackFontScale) private var scale

    public enum Kind { case new, stale }

    public init(_ text: String, kind: Kind = .new) {
        self.text = text
        self.kind = kind
    }

    public var body: some View {
        let palette = TrackTheme.palette(for: scheme)
        let ink: Color = kind == .new ? palette.mark : palette.faint
        let ground: Color = kind == .new ? palette.mark.opacity(0.12) : palette.panelSoft
        Text(text)
            .font(.system(size: 10 * scale, weight: .medium, design: .monospaced))
            .tracking(0.8 * scale)
            .textCase(.uppercase)
            .foregroundStyle(ink)
            .padding(.horizontal, 6).padding(.vertical, 1)
            .background(ground, in: RoundedRectangle(cornerRadius: 4))
    }
}

/// Author-assigned flag chip (web `.note-flag-badge`): same label typography
/// as the state badge, but the author's own permanent marker in danger red.
public struct TrackFlagBadge: View {
    public let text: String
    @Environment(\.colorScheme) private var scheme
    @Environment(\.trackFontScale) private var scale

    public init(_ text: String) { self.text = text }

    public var body: some View {
        let palette = TrackTheme.palette(for: scheme)
        Text(text)
            .font(.system(size: 10 * scale, weight: .medium, design: .monospaced))
            .tracking(0.8 * scale)
            .textCase(.uppercase)
            .foregroundStyle(palette.danger)
            .padding(.horizontal, 6).padding(.vertical, 1)
            .background(palette.danger.opacity(0.12), in: RoundedRectangle(cornerRadius: 4))
    }
}

public extension TrackTheme {
    /// Heatmap step for a day count (web heatmap: 5 levels, chart-1 mixed over
    /// panel-soft, full chart-1 at the top; empty is the sunk fill).
    func heatColor(count: Int, max: Int = 8) -> Color {
        if count <= 0 { return panelSoft }
        let t: Double
        if max <= 1 { t = 1 }
        else { t = min(1, Double(count) / Double(max)) }
        // Five visual steps matching the web's 28/50/72/100 mixes.
        let mix: Double
        switch t {
        case ..<0.25: mix = 0.28
        case ..<0.5: mix = 0.50
        case ..<0.75: mix = 0.72
        default: mix = 1.0
        }
        return Self.mix(panelSoft, chartPalette[0], t: mix)
    }

    /// sRGB linear mix of two SwiftUI colors (ratio of `b`).
    static func mix(_ a: Color, _ b: Color, t: Double) -> Color {
        let ca = NSColor(a).usingColorSpace(.sRGB) ?? NSColor.gray
        let cb = NSColor(b).usingColorSpace(.sRGB) ?? NSColor.gray
        var r1: CGFloat = 0, g1: CGFloat = 0, bl1: CGFloat = 0, a1: CGFloat = 0
        var r2: CGFloat = 0, g2: CGFloat = 0, bl2: CGFloat = 0, a2: CGFloat = 0
        ca.getRed(&r1, green: &g1, blue: &bl1, alpha: &a1)
        cb.getRed(&r2, green: &g2, blue: &bl2, alpha: &a2)
        let tt: CGFloat = CGFloat(min(1, max(0, t)))
        let r = r1 + (r2 - r1) * tt
        let g = g1 + (g2 - g1) * tt
        let bl = bl1 + (bl2 - bl1) * tt
        return Color(nsColor: NSColor(srgbRed: r, green: g, blue: bl, alpha: 1))
    }

    /// Hex strings for the figure-island bridge (web `getComputedStyle` reads
    /// the same resolved tokens for ECharts/Mermaid/graph). Colors, not chrome:
    /// the island needs the visualization palette, not just bg/fg.
    struct CSS: Sendable {
        let bg: String
        let fg: String
        let panelSoft: String
        let lineStrong: String
        let mark: String
        let danger: String
        let chart: [String]
        let rampLo: String
        let rampHi: String
    }

    static func css(for scheme: ColorScheme) -> CSS {
        switch scheme {
        case .dark:
            return CSS(
                bg: "#141618", fg: "#e9e9e4", panelSoft: "#212528",
                lineStrong: "#3e4347", mark: "#f4785e", danger: "#de766b",
                chart: ["#74c4a8", "#dca06a", "#9bb7d5", "#dc8b84", "#b5c383", "#c1a4c6"],
                rampLo: "#28322f", rampHi: "#91d0b8"
            )
        default:
            return CSS(
                bg: "#fbfaf8", fg: "#1a1a18", panelSoft: "#f3f2ee",
                lineStrong: "#c7c5bd", mark: "#c13a1e", danger: "#8a352b",
                chart: ["#286957", "#a05f2e", "#536f91", "#99504a", "#737b4a", "#795f80"],
                rampLo: "#e4ebe7", rampHi: "#286957"
            )
        }
    }
}
