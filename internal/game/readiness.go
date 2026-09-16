package game

import p "competition/internal/protocol"

// Readiness checks local geometry and provenance declarations, not judge acceptance.
// Profiles are supplied by the operator after checking the official build mask.
func (c Config) Readiness(r p.Request) []string {
	var issues []string
	if err := c.Validate(); err != nil {
		issues = append(issues, err.Error())
	}
	if c.ExperimentalDemoLayout {
		issues = append(issues, "experimental demo layout is enabled")
	}
	f, ok := c.Profiles[r.Our.Type]
	if !ok || !f.Verified {
		issues = append(issues, "missing verified profile for "+r.Our.Type)
		return issues
	}
	if len(f.Weapons) == 0 {
		issues = append(issues, "no weapon sites configured")
	}
	g := staticGrid(r)
	for _, s := range f.Weapons {
		if !r.In(s.Pos) {
			issues = append(issues, "weapon site outside map")
			continue
		}
		if !g.Free(s.Pos) {
			issues = append(issues, "weapon site is occupied in supplied snapshot")
		}
		if len(g.Around([]p.Pos{s.Pos})) == 0 {
			issues = append(issues, "weapon site has no operating position")
		}
	}
	for _, q := range f.Walls {
		if !r.In(q) {
			issues = append(issues, "wall site outside map")
		}
	}
	return issues
}
