package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
	"strconv"
)

func fallback(g nav.Grid, start nav.State, gs [][]int, roles []p.Role, follow bool, out *p.Response) {
	next := start
	for i, u := range roles {
		if _, busy := out.Commands[strconv.Itoa(u.ID)]; busy {
			continue
		}
		grid := g.Clone()
		for j := range roles {
			if i != j {
				grid.Block[start[j]] = true
				grid.Block[next[j]] = true
			}
		}
		path := grid.Shortest(start[i], gs[i])
		if len(path) > 1 {
			next[i] = path[1]
		}
	}
	if nav.ValidTransition(start, next, len(roles), follow) {
		for i, u := range roles {
			if next[i] != start[i] {
				out.Commands[strconv.Itoa(u.ID)] = p.At("move", g.Pos(next[i]))
			}
		}
	}
}
func zones(r p.Request, kind string) []p.Pos {
	var a []p.Pos
	for _, z := range r.Map.Zones {
		if z.Type == kind {
			a = append(a, z.Pos)
		}
	}
	return a
}
func near(pos p.Pos, cells []p.Pos) bool {
	for _, q := range cells {
		if p.Distance(pos, q) <= 1 {
			return true
		}
	}
	return false
}
func moveOr(r p.Request, u p.Role, g nav.Grid, cells []p.Pos, cmd p.Command, out *p.Response, goals map[int][]int) bool {
	gs := g.Around(cells)
	if cmd.Action == "build" {
		filtered := gs[:0]
		for _, id := range gs {
			target := false
			for _, cell := range cells {
				if g.Pos(id) == cell {
					target = true
				}
			}
			if !target {
				filtered = append(filtered, id)
			}
		}
		gs = filtered
	}
	path := g.Shortest(g.ID(u.Pos), gs)
	if path == nil {
		return false
	}
	if len(path) == 1 {
		out.Commands[strconv.Itoa(u.ID)] = cmd
	} else {
		goals[u.ID] = gs
	}
	return true
}

func buildConnected(r p.Request, g nav.Grid, q p.Pos) bool {
	if !g.In(q) {
		return false
	}
	h := g.Clone()
	h.Block[h.ID(q)] = true
	destinations := [][]p.Pos{zones(r, "vendor"), zones(r, "weaponShop")}
	for _, site := range r.Our.Tasks {
		destinations = append(destinations, taskCells(r, site))
	}
	for _, w := range r.Weapons() {
		destinations = append(destinations, []p.Pos{w.Pos})
	}
	for _, u := range r.Mobiles() {
		if !h.Free(u.Pos) {
			return false
		}
		for _, targets := range destinations {
			if len(targets) > 0 && g.Shortest(g.ID(u.Pos), g.Around(targets)) != nil && h.Shortest(h.ID(u.Pos), h.Around(targets)) == nil {
				return false
			}
		}
	}
	return true
}
