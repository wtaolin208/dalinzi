package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
)

func pioneer(r p.Request, u p.Role, g nav.Grid, c Config, m *Memory, out *p.Response, goals map[int][]int, gold *int, traces ...*Trace) bool {
	if !r.Daylight() || baseEmergency(r, c) {
		return false
	}

	selected := chooseTask(r, u, g, c, *m, traces...)
	if selected == nil {
		return planTreasure(r, u, g, c, m, out, goals, gold)
	}
	cmd := p.Command{Action: "acceptTask"}
	if moveOr(r, u, g, taskCells(r, *selected), cmd, out, goals) {
		if near(u.Pos, taskCells(r, *selected)) {
			m.Task.Position = selected.Pos
			m.Task.Start = r.Round
			if selected.Timeout != nil {
				m.Task.Timeout = *selected.Timeout
			}
		}
		return true
	}
	return false
}

func chooseTask(r p.Request, u p.Role, g nav.Grid, c Config, m Memory, traces ...*Trace) *p.PlayerTask {
	var selected *p.PlayerTask
	score := -1.0
	for _, site := range r.Our.Tasks {
		if !site.Valid || site.Cooldown > 0 {
			continue
		}
		path := g.Shortest(g.ID(u.Pos), g.Around(taskCells(r, site)))
		if path == nil {
			continue
		}
		a := assessTask(r, g, c, m, site, len(path)-1, g.Pos(path[len(path)-1]))
		if len(traces) > 0 {
			traces[0].Tasks = append(traces[0].Tasks, a)
		}
		if !a.Safe {
			continue
		}
		v := a.Value
		if v > score {
			x := site
			selected = &x
			score = v
		}
	}
	return selected
}
