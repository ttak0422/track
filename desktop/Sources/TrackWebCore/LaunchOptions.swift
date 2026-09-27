import Foundation

public struct LaunchOptions: Equatable, Sendable {
    public let vaultName: String?
    public let vaultPath: String?

    public init(arguments: [String]) throws {
        var name: String?
        var path: String?
        var index = 0

        while index < arguments.count {
            let argument = arguments[index]
            let value: String
            if argument == "--vault" || argument == "--vault-path" {
                index += 1
                guard index < arguments.count else {
                    throw LaunchOptionsError.missingValue(argument)
                }
                value = arguments[index]
                if argument == "--vault" {
                    guard name == nil else { throw LaunchOptionsError.duplicate(argument) }
                    name = value
                } else {
                    guard path == nil else { throw LaunchOptionsError.duplicate(argument) }
                    path = value
                }
            } else if argument.hasPrefix("--vault=") {
                guard name == nil else { throw LaunchOptionsError.duplicate("--vault") }
                name = String(argument.dropFirst("--vault=".count))
            } else if argument.hasPrefix("--vault-path=") {
                guard path == nil else { throw LaunchOptionsError.duplicate("--vault-path") }
                path = String(argument.dropFirst("--vault-path=".count))
            } else {
                throw LaunchOptionsError.unknown(argument)
            }
            index += 1
        }

        guard name == nil || path == nil else { throw LaunchOptionsError.conflictingVaultSelection }
        if let name, name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            throw LaunchOptionsError.emptyValue("--vault")
        }
        if let path {
            guard !path.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
                throw LaunchOptionsError.emptyValue("--vault-path")
            }
            guard (path as NSString).isAbsolutePath else {
                throw LaunchOptionsError.vaultPathMustBeAbsolute(path)
            }
        }

        self.vaultName = name
        self.vaultPath = path
    }

    public var trackArguments: [String] {
        var result: [String] = []
        if let vaultName {
            result += ["--vault", vaultName]
        }
        result += ["web", "--addr", "127.0.0.1:18765"]
        return result
    }

    public var environmentOverrides: [String: String] {
        guard let vaultPath else { return [:] }
        return ["TRACK_VAULT": vaultPath]
    }
}

public enum LaunchOptionsError: Error, Equatable, LocalizedError {
    case missingValue(String)
    case duplicate(String)
    case unknown(String)
    case conflictingVaultSelection
    case emptyValue(String)
    case vaultPathMustBeAbsolute(String)

    public var errorDescription: String? {
        switch self {
        case .missingValue(let option): return "\(option) needs a value"
        case .duplicate(let option): return "\(option) may be supplied only once"
        case .unknown(let option): return "unsupported launch argument: \(option)"
        case .conflictingVaultSelection: return "choose either --vault or --vault-path, not both"
        case .emptyValue(let option): return "\(option) cannot be empty"
        case .vaultPathMustBeAbsolute(let path): return "vault path must be absolute: \(path)"
        }
    }
}
