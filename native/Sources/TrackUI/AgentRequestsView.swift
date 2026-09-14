import SwiftUI
import MarkdownUI
import TrackAPI

/// A sheet or workspace panel. The target's vault is captured when opened, so
/// changing the working vault cannot redirect an in-flight request or save.
public struct AgentRequestsView: View {
    private let client: TrackClient
    private let initialTarget: AgentRequestTarget?
    private let onOpenNote: (TrackID) -> Void
    @State private var model: AgentRequestsModel?
    @Environment(VaultScope.self) private var vaultScope: VaultScope?
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.dismiss) private var dismiss

    public init(client: TrackClient, target: AgentRequestTarget? = nil, onOpenNote: @escaping (TrackID) -> Void = { _ in }) {
        self.client = client
        initialTarget = target
        self.onOpenNote = onOpenNote
    }

    public var body: some View {
        Group {
            if let model { panel(model) }
            else { ProgressView("Loading requests…") }
        }
        .frame(minWidth: 660, minHeight: 560)
        .task {
            if model == nil {
                model = AgentRequestsModel(client: client, target: initialTarget ?? AgentRequestTarget(vault: vaultScope?.scope ?? ""))
            }
            await model?.loadAgents()
            await model?.refresh()
        }
        .task(id: scenePhase) {
            guard scenePhase == .active else { return }
            // Same 2-second history refresh as Web, cancelled when the panel closes.
            while !Task.isCancelled {
                await model?.refresh()
                do { try await Task.sleep(for: .seconds(2)) } catch { break }
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: .trackVaultChanged)) { _ in
            guard scenePhase == .active else { return }
            Task { await model?.refresh() }
        }
    }

    private func panel(_ model: AgentRequestsModel) -> some View {
        VStack(spacing: 0) {
            HStack {
                VStack(alignment: .leading, spacing: 3) {
                    Text("Agent requests").font(.title2.weight(.semibold))
                    Text(model.target.title + (model.target.vault.isEmpty ? "" : " · " + model.target.vault))
                        .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }
                Spacer()
                Button("Refresh", systemImage: "arrow.clockwise") {
                    Task { await model.loadAgents(); await model.refresh() }
                }.disabled(model.isWorking || model.isRefreshing)
                Button("Close", systemImage: "xmark") { dismiss() }
                    .keyboardShortcut(.cancelAction)
            }.padding()
            Divider()
            HSplitView {
                ScrollView { composer(model).padding() }
                    .frame(minWidth: 260, idealWidth: 290, maxWidth: 360)
                ScrollView {
                    VStack(alignment: .leading, spacing: 18) {
                        if let error = model.refreshError {
                            Label(error, systemImage: "exclamationmark.triangle")
                                .font(.callout).foregroundStyle(.red).textSelection(.enabled)
                        }
                        if let selected = model.selected { result(selected, model: model) }
                        Text("History").trackSectionLabel()
                        if model.requests.isEmpty {
                            Text(model.isRefreshing ? "Loading…" : "No requests in this vault.").foregroundStyle(.secondary)
                        }
                        ForEach(model.requests) { request in
                            requestRow(request, model: model)
                        }
                        if model.nextCursor?.isEmpty == false {
                            Button("Load older requests") { Task { await model.refresh(loadMore: true) } }
                                .disabled(model.isRefreshing || model.isWorking)
                        }
                    }.frame(maxWidth: .infinity, alignment: .leading).padding()
                }.frame(minWidth: 340)
            }
            if let error = model.actionError {
                Divider()
                Label(error, systemImage: "exclamationmark.triangle")
                    .foregroundStyle(.red).textSelection(.enabled).padding()
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }

    private func composer(_ model: AgentRequestsModel) -> some View {
        @Bindable var model = model
        return VStack(alignment: .leading, spacing: 14) {
            Text("New request").trackSectionLabel()
            Picker("Operation", selection: $model.intent) {
                ForEach(RequestIntent.allCases, id: \.self) { Text($0.label).tag($0) }
            }
            .onChange(of: model.intent) { _, _ in model.chooseAgent() }
            if !model.target.quote.isEmpty {
                DisclosureGroup("Selected text") {
                    Text(model.target.quote).font(.callout).textSelection(.enabled)
                }
            }
            if let parent = model.parent {
                Text("Follow-up to: \(parent.instruction)").font(.callout).foregroundStyle(.secondary)
                Button("Start a separate request") { model.clearFollowUp() }
            }
            if model.intent == .update {
                Text(model.target.note == nil ? "Open a note to request an update." : "Updates apply automatically to this note. Changes made since it was read cause a conflict.")
                    .font(.callout).foregroundStyle(.secondary)
            }
            Text("Instruction").font(.caption)
            TextEditor(text: $model.instruction)
                .frame(minHeight: 140)
                .padding(6)
                .overlay(RoundedRectangle(cornerRadius: 6).stroke(.separator))
                .accessibilityLabel("Agent instruction")
            Picker("Agent", selection: $model.agentID) {
                Text("Select an agent").tag("")
                ForEach(model.availableAgents) { agent in
                    Text((agent.name ?? agent.id) + (agent.agmsgAvailable ? "" : " (unavailable)"))
                        .tag(agent.id).disabled(!agent.agmsgAvailable)
                }
            }
            if let error = model.agentsError { Text(error).font(.callout).foregroundStyle(.red).textSelection(.enabled) }
            if !model.availableAgents.contains(where: \.agmsgAvailable) {
                Text("No available agent supports this operation. Check the vault’s agent configuration and refresh.")
                    .font(.callout).foregroundStyle(.secondary)
            }
            Button(model.isWorking ? "Working…" : "Send \(model.intent.label.lowercased()) request") {
                Task { await model.send() }
            }
            .buttonStyle(.borderedProminent)
            .disabled(!model.canSend)
        }
        .disabled(model.isWorking)
    }

    private func requestRow(_ request: AgentRequest, model: AgentRequestsModel) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Button { model.select(request) } label: {
                VStack(alignment: .leading, spacing: 5) {
                    HStack {
                        Text(request.intent.label).fontWeight(.medium)
                        Spacer()
                        Text(request.status.capitalized).foregroundStyle(.secondary)
                    }
                    Text(request.instruction).lineLimit(3).multilineTextAlignment(.leading)
                }.contentShape(Rectangle())
            }.buttonStyle(.plain).disabled(model.isWorking)
            if let error = request.error { Text(error).font(.caption).foregroundStyle(.red).textSelection(.enabled) }
            HStack {
                if request.canCancel { Button("Cancel") { Task { await model.cancel(request) } } }
                if request.canRetry { Button("Retry") { Task { await model.retry(request) } } }
                if request.result != nil { Button("Follow up") { model.followUp(request) } }
            }.controlSize(.small).disabled(model.isWorking)
            Divider()
        }
    }

    private func result(_ request: AgentRequest, model: AgentRequestsModel) -> some View {
        @Bindable var model = model
        return VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Result · \(request.status.capitalized)").font(.headline)
                Spacer()
                if let target = request.updateTarget { Button("Open target") { onOpenNote(target.noteID) } }
            }
            if request.intent == .update {
                sourceBlock("Proposed body", text: request.result?.proposedBody ?? "No proposed body.")
                if let apply = request.result?.apply {
                    if let reason = apply.reason { Text(reason).textSelection(.enabled) }
                    if let date = apply.appliedAt { Text("Applied: \(date)").font(.caption).foregroundStyle(.secondary) }
                    sourceBlock("Before", text: apply.beforeBody ?? "No recorded body.")
                    sourceBlock("After", text: apply.afterBody ?? (request.status == "completed" ? "" : "No update was applied."))
                    if let etag = apply.beforeEtag { Text("Before ETag: \(etag)").font(.caption).textSelection(.enabled) }
                    if let etag = apply.afterEtag { Text("After ETag: \(etag)").font(.caption).textSelection(.enabled) }
                } else {
                    Text(request.error ?? (request.status == "completed" ? "No application details were recorded." : "The update has not been applied."))
                        .textSelection(.enabled)
                }
            } else if let answer = request.result?.answerMarkdown {
                Markdown(answer).textSelection(.enabled)
            } else {
                Text(request.error ?? "Waiting for the agent’s answer.").foregroundStyle(.secondary)
            }
            if let output = request.result {
                stringList("Sources", items: output.sources ?? [])
                stringList("Unresolved", items: output.unresolved)
                stringList("Uncertain", items: output.uncertain)
                if let saved = output.saved {
                    Button("Open saved note: \(saved.title)") { onOpenNote(saved.noteID) }
                }
            }
            if request.canSave {
                Text("Save answer as a note").font(.headline)
                TextField("Note title", text: $model.saveTitle)
                TextField("Destination vault (empty: request’s vault)", text: $model.saveVault)
                Button("Save answer") { Task { await model.saveAnswer() } }
                    .disabled(model.isWorking || model.saveTitle.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            if let attempts = request.attempts, !attempts.isEmpty {
                DisclosureGroup("Attempts (\(attempts.count))") {
                    ForEach(attempts) { Text("\($0.id): \($0.status)").font(.caption).textSelection(.enabled) }
                }
            }
            if request.result != nil { Button("Follow up") { model.followUp(request) }.disabled(model.isWorking) }
            Divider()
        }
    }

    private func sourceBlock(_ title: String, text: String) -> some View {
        DisclosureGroup(title) {
            Text(text).font(.system(.callout, design: .monospaced)).textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, 6)
        }
    }
    @ViewBuilder
    private func stringList(_ title: String, items: [String]) -> some View {
        if !items.isEmpty {
            VStack(alignment: .leading, spacing: 5) {
                Text(title).font(.subheadline.weight(.medium))
                ForEach(Array(items.enumerated()), id: \.offset) { _, item in Text("• \(item)").textSelection(.enabled) }
            }
        }
    }
}
