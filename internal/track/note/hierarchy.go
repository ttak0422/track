package note

// UpProp is the conventional relation property that places a note in a hierarchy: "up:: [[Parent]]"
// (inline field) or an "up" sidecar prop whose value is a [[link]]. The property index already
// captures it like any other typed property; this constant just names the convention so the trail
// walk (store), the static bundle, and the docs all agree on one key.
const UpProp = "up"

// UpTargets returns the resolution keys a note's "up" properties point at, in property order,
// deduplicated. Only link-typed values count: "up:: draft" is a string, not a parent.
func UpTargets(props []Prop) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range props {
		if p.Key != UpProp || p.Type != TypeLink || seen[p.Value] {
			continue
		}
		seen[p.Value] = true
		out = append(out, p.Value)
	}
	return out
}
