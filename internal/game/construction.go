package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
	"fmt"
	"sort"
)

// Compare eight-neighbour corridors from observed traffic, without assuming
// unverified destructible-wall or projectile-blocking mechanics.
func wallPlanValue(r p.Request, g nav.Grid, m Memory, q p.Pos) (float64, bool) {
	type entry struct {
		key   string
		count int
	}
	entries := []entry{}
	for key, count := range m.RobotHeat {
		entries = append(entries, entry{key, count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count == entries[j].count {
			return entries[i].key < entries[j].key
		}
		return entries[i].count > entries[j].count
	})
	if len(entries) > 8 {
		entries = entries[:8]
	}
	var base []p.Pos
	for _, u := range r.Our.Roles {
		if u.Type == "station" && u.Health > 0 {
			base = append(base, u.Cells()...)
		}
	}
	if len(base) == 0 {
		return 40, true
	}
	modified := g.Clone()
	modified.Block[g.ID(q)] = true
	nearWeapon := func(grid nav.Grid, path []int) bool {
		for _, id := range path {
			for _, w := range r.Weapons() {
				if p.Distance(grid.Pos(id), w.Pos) <= 1 {
					return true
				}
			}
		}
		return false
	}
	for _, entry := range entries {
		var start p.Pos
		if _, err := fmt.Sscanf(entry.key, "%d,%d", &start.X, &start.Y); err != nil || !g.Free(start) || start == q {
			continue
		}
		before := g.Shortest(g.ID(start), g.Around(base))
		if len(before) == 0 {
			continue
		}
		after := modified.Shortest(g.ID(start), modified.Around(base))
		if len(after) == 0 || len(after) < len(before) || !nearWeapon(g, before) && nearWeapon(modified, after) {
			return 0, false
		}
	}
	// Preserve frequently used firing corridors; build low-traffic closures first.
	return 40 / (1 + float64(m.RobotHeat[siteKey(q)])), true
}
