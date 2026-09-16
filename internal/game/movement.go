package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
	"strconv"
)

// Scores are conservative heuristics, not probabilities of enemy actions.
type MoveAssessment struct {
	Role     int   `json:"role"`
	Planned  p.Pos `json:"planned"`
	Selected p.Pos `json:"selected"`
	Risk     int   `json:"risk"`
	Stuck    int   `json:"stuck"`
}

// Only this turn's reachable cells receive risk. Current occupied cells remain
// blocked by staticGrid; no assumption is made that an opponent will vacate them.
func movementRisk(r p.Request, g nav.Grid) []int {
	risk := make([]int, len(g.Block))
	add := func(pos p.Pos) {
		if !g.In(pos) {
			return
		}
		for _, id := range g.Neighbors(g.ID(pos), false) {
			risk[id] = min(24, risk[id]+12)
		}
	}
	for _, u := range r.Enemy.Roles {
		if u.Health > 0 && u.Mobile() {
			add(u.Pos)
		}
	}
	for _, u := range r.Robots.Roles {
		if u.Health > 0 {
			add(u.Pos)
		}
	}
	return risk
}

// Re-rank at most 9^3 joint first steps after static planning (including its
// budget fallback). Future enemy positions are unknown, so only distance to the
// goal is used beyond the first step. Existing actions and goal occupants stay put.
func assessMovement(r p.Request, g nav.Grid, start nav.State, goals [][]int, roles []p.Role, locked [3]bool, follow bool, m Memory, out *p.Response, tr *Trace, priorities ...int) {
	if len(roles) == 0 || len(roles) > 3 {
		return
	}
	risk := movementRisk(r, g)
	for id := range risk {
		risk[id] += m.CollisionHeat[siteKey(g.Pos(id))]
	}
	baseline := start
	dist := make([][]int, len(roles))
	moves := make([][]int, len(roles))
	active := false
	for i, u := range roles {
		key := strconv.Itoa(u.ID)
		cmd, exists := out.Commands[key]
		dist[i] = g.Distances(goals[i])
		if exists && cmd.Action != "move" {
			locked[i] = true
		}
		if dist[i][start[i]] <= 0 {
			locked[i] = true
		}
		moves[i] = []int{start[i]}
		if locked[i] {
			continue
		}
		if cmd.Action == "move" && len(cmd.Targets) == 1 && g.Free(cmd.Targets[0]) {
			baseline[i] = g.ID(cmd.Targets[0])
		}
		for _, id := range g.Neighbors(start[i], false) {
			// Permit a one-step retreat to escape a blockage, but no unbounded detour.
			if dist[i][id] >= 0 && dist[i][id] <= dist[i][start[i]]+1 {
				moves[i] = append(moves[i], id)
				active = active || risk[id] > 0
			}
		}
		active = active || m.Stuck[u.ID] >= 2
	}
	if !active {
		return
	}
	score := func(s nav.State) int {
		v := 0
		for i, u := range roles {
			if locked[i] {
				continue
			}
			v += 10 * dist[i][s[i]]
			if s[i] == start[i] {
				// Waiting avoids moving collisions but must not become a cheap
				// permanent substitute for progressing through a risky passage.
				v += 18
				if m.Stuck[u.ID] >= 2 {
					v += 22
				}
			} else {
				v += risk[s[i]]
				prev := m.LastResponse.Commands[strconv.Itoa(u.ID)]
				if m.Stuck[u.ID] >= 2 && prev.Action == "move" && len(prev.Targets) == 1 && g.Pos(s[i]) == prev.Targets[0] {
					v += 40
				}
			}
		}
		return v
	}
	best := baseline
	bestScore := int(^uint(0) >> 1)
	if nav.ValidTransition(start, baseline, len(roles), follow) {
		bestScore = score(baseline)
	}
	next := start
	var visit func(int)
	visit = func(i int) {
		if i < len(roles) {
			for _, id := range moves[i] {
				next[i] = id
				visit(i + 1)
			}
			return
		}
		if !nav.ValidTransition(start, next, len(roles), follow) {
			return
		}
		// Preserve the priority role's planned step unless that step itself is
		// risky or repeatedly blocked. Other roles may still adjust around it.
		if len(priorities) > 0 && priorities[0] > 0 {
			p := priorities[0] - 1
			if p < len(roles) && risk[baseline[p]] == 0 && m.Stuck[roles[p].ID] < 2 && next[p] != baseline[p] {
				return
			}
		}
		if s := score(next); s < bestScore {
			best, bestScore = next, s
		}
	}
	visit(0)
	for i, u := range roles {
		if locked[i] {
			continue
		}
		key := strconv.Itoa(u.ID)
		delete(out.Commands, key)
		if best[i] != start[i] {
			out.Commands[key] = p.At("move", g.Pos(best[i]))
		}
		tr.Movement = append(tr.Movement, MoveAssessment{u.ID, g.Pos(baseline[i]), g.Pos(best[i]), risk[best[i]], m.Stuck[u.ID]})
	}
}
