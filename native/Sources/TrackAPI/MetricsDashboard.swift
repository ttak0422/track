import Foundation

public struct MetricsDashboardResponse: Decodable, Sendable {
    public let title: String
    public let entities: [String]
    public let metrics: [String]
    public let asof: String
    public let panels: [MetricsDashboardPanel]
}

public struct MetricsDashboardPanel: Decodable, Sendable, Identifiable {
    public struct Grid: Decodable, Sendable {
        public let x: Int
        public let y: Int
        public let w: Int
        public let h: Int
    }

    public struct Value: Decodable, Sendable {
        public let name: String
        public let entity: String
        public let value: Double
        public let time: String
        public let display: String
        public let threshold: Double?
        public let thresholdDisplay: String?

        public var state: String {
            threshold.map { "Threshold ≥ \(thresholdDisplay ?? $0.formatted())" } ?? "Baseline"
        }
    }

    public let id: String
    public let title: String
    public let type: String
    public let grid: Grid
    public let unit: String
    public let values: [Value]
    public let echartsJSON: String?
    public let error: String?

    enum CodingKeys: String, CodingKey { case id, title, type, grid, unit, values, echarts, error }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        title = try c.decode(String.self, forKey: .title)
        type = try c.decode(String.self, forKey: .type)
        grid = try c.decode(Grid.self, forKey: .grid)
        unit = try c.decodeIfPresent(String.self, forKey: .unit) ?? ""
        values = try c.decodeIfPresent([Value].self, forKey: .values) ?? []
        error = try c.decodeIfPresent(String.self, forKey: .error)
        if try c.contains(.echarts) && !c.decodeNil(forKey: .echarts) {
            echartsJSON = try ViewSpecResponse(from: decoder).echartsJSON
        } else {
            echartsJSON = nil
        }
    }
}
