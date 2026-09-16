package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
	"context"
)

func defense(ctx context.Context, r p.Request, g nav.Grid, c Config) ([]Pair, []PathTrace) {
	roles := r.Mobiles()
	ws := r.Weapons()
	fixed := map[int]int{}
	if c.Strategy.FixedStations && len(roles) == 3 && len(ws) == 3 && r.PhaseTask == "" {
		worker := 0
		for _, u := range roles {
			kind := "rocket"
			if u.Type == "worker" {
				kind = "gatling"
				if worker > 0 {
					kind = "railgun"
				}
				worker++
			}
			for _, w := range ws {
				if w.Type == kind {
					fixed[u.ID] = w.ID
					break
				}
			}
		}
		seen := map[int]bool{}
		for _, id := range fixed {
			seen[id] = true
		}
		if len(fixed) != 3 || len(seen) != 3 {
			fixed = map[int]int{}
		}
	}
	available := 0
	for _, u := range roles {
		if !(u.Type == "pioneer" && r.PhaseTask != "") {
			available++
		}
	}
	n := min(available, len(ws))
	if n == 0 {
		return nil, nil
	}
	bestCost := int(^uint(0) >> 1)
	bestFire := -1.0
	potential := map[int]float64{}
	if !r.Daylight() && n < len(ws) {
		for _, w := range ws {
			for _, shot := range candidates(r, w, c) {
				potential[w.ID] = max(potential[w.ID], shot.Value)
			}
		}
	}
	var best []Pair
	var traces []PathTrace
	used := make([]bool, len(roles))
	chosen := make([]Pair, n)
	var visit func(int, int)
	visit = func(k int, firstWeapon int) {
		if ctx.Err() != nil {
			return
		}
		if k < n {
			for wi := firstWeapon; wi <= len(ws)-(n-k); wi++ {
				for j := range roles {
					if id, ok := fixed[roles[j].ID]; ok && id != ws[wi].ID {
						continue
					}
					if !used[j] && !(roles[j].Type == "pioneer" && r.PhaseTask != "") {
						used[j] = true
						chosen[k] = Pair{roles[j], ws[wi]}
						visit(k+1, wi+1)
						used[j] = false
					}
				}
			}
			return
		}
		start := nav.State{}
		gs := make([][]int, len(roles))
		ids := make([]int, len(roles))
		assigned := make([]int, len(roles))
		locked := [3]bool{}
		for i, u := range roles {
			start[i] = g.ID(u.Pos)
			ids[i] = u.ID
			gs[i] = []int{start[i]}
			locked[i] = u.Type == "pioneer" && r.PhaseTask != ""
			for _, pair := range chosen {
				if pair.Role.ID == u.ID {
					gs[i] = g.Around([]p.Pos{pair.Weapon.Pos})
					assigned[i] = pair.Weapon.ID
				}
			}
		}
		res := nav.Joint(ctx, g, start, gs, nav.Options{MaxExpanded: max(100, c.MaxExpanded/6), MaxStates: c.MaxStates, Follow: c.FollowMoves, Locked: locked})
		traces = append(traces, PathTrace{ids, gs, res, assigned})
		cost := res.Cost
		if cost < 0 {
			cost = 100000
			for i := range roles {
				d := g.Distances(gs[i])
				v := d[start[i]]
				if v < 0 {
					v = 10000
				}
				cost += v
			}
		}
		fire := 0.0
		for _, pair := range chosen {
			if p.Distance(pair.Role.Pos, pair.Weapon.Pos) <= 1 {
				fire += potential[pair.Weapon.ID]
			}
		}
		if fire > bestFire || fire == bestFire && cost < bestCost {
			bestFire = fire
			bestCost = cost
			best = append([]Pair(nil), chosen...)
		}
	}
	visit(0, 0)
	if len(fixed) > 0 && bestCost >= 100000 && ctx.Err() == nil {
		c.Strategy.FixedStations = false
		return defense(ctx, r, g, c)
	}
	return best, traces
}
