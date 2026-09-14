import SwiftUI
import TrackAPI

/// Working-vault actions follow this selection; workspace search and open notes
/// retain their own scope, matching web/vaultScope.tsx.
@MainActor
@Observable
public final class VaultScope {
    public static let storageKey = "track.vault-scope"
    public private(set) var scope: String
    public private(set) var activeName = ""
    public private(set) var vaults: [VaultEntry] = []
    public private(set) var unavailable: [UnavailableVault] = []
    public private(set) var isLoading = false
    public private(set) var error: String?
    private let client: TrackClient
    private let defaults: UserDefaults

    public init(client: TrackClient, defaults: UserDefaults = .standard) {
        self.client = client
        self.defaults = defaults
        scope = defaults.string(forKey: Self.storageKey) ?? ""
    }

    public var label: String {
        let name = scope.isEmpty ? activeName : scope
        return name.isEmpty ? "local" : name
    }

    public func select(_ name: String) {
        scope = name
        if name.isEmpty { defaults.removeObject(forKey: Self.storageKey) }
        else { defaults.set(name, forKey: Self.storageKey) }
    }

    public func reload() async {
        guard !isLoading else { return }
        isLoading = true
        defer { isLoading = false }
        do {
            let response = try await client.listVaults()
            activeName = response.active.name
            vaults = response.vaults
            unavailable = response.unavailable ?? []
            if !scope.isEmpty, !vaults.contains(where: { $0.name == scope }) { select("") }
            error = nil
        } catch {
            self.error = (error as? APIError)?.message ?? error.localizedDescription
        }
    }
}

public struct VaultSwitcher: View {
    @Bindable private var model: VaultScope

    public init(model: VaultScope) { self.model = model }

    public var body: some View {
        Menu {
            ForEach(model.vaults, id: \.name) { vault in
                Button {
                    model.select(vault.active ? "" : vault.name)
                } label: {
                    let selected = model.scope.isEmpty ? vault.active : model.scope == vault.name
                    Label(vault.name.isEmpty ? "local" : vault.name,
                          systemImage: selected ? "checkmark" : "folder")
                }
                .help(vault.path)
            }
            ForEach(model.unavailable, id: \.name) { vault in
                Text("\(vault.name) — unavailable\(vault.error.map { ": \($0)" } ?? "")")
            }
            if let error = model.error { Text(error) }
            Divider()
            Button("Refresh vaults") { Task { await model.reload() } }
                .disabled(model.isLoading)
        } label: {
            Label(model.label, systemImage: model.error == nil ? "folder" : "exclamationmark.triangle")
        }
        .help("Working vault: \(model.label)")
        .accessibilityLabel("Working vault: \(model.label). Change working vault")
    }
}
