package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type workOption struct {
	key                    string
	command                p.Command
	goals                  []int
	distance, gold, towers int
	value                  float64
	exclusive              string
}
type WorkTrace struct {
	Assignments map[int]string `json:"assignments"`
	Evaluated   int            `json:"evaluated"`
	Expanded    int            `json:"expanded"`
	Proven      bool           `json:"provenWithinCandidates"`
	Reason      string         `json:"reason"`
}

func taskPriority(r p.Request, m Memory, roles []p.Role, goals map[int][]int, out p.Response) int {
	if !r.Daylight() || r.PhaseTask != "" || m.Task.Abandon || baseEmergency(r) {
		return 0
	}
	for i, u := range roles {
		if u.Type != "pioneer" || len(goals[u.ID]) == 0 {
			continue
		}
		if _, busy := out.Commands[strconv.Itoa(u.ID)]; busy {
			continue
		}
		// Only task travel gets priority, not shopping/treasure or routine defense.
		for _, site := range r.Our.Tasks {
			if site.Valid && site.Cooldown == 0 {
				g := staticGrid(r)
				target := g.Around(taskCells(r, site))
				if sameGoals(goals[u.ID], target) {
					return i + 1
				}
			}
		}
	}
	return 0
}
func sameGoals(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[int]bool{}
	for _, v := range a {
		seen[v] = true
	}
	for _, v := range b {
		if !seen[v] {
			return false
		}
	}
	return true
}

func workerOptions(ctx context.Context, r p.Request, u p.Role, g nav.Grid, c Config, gold int, previous string) []workOption {
	var choices []workOption
	add := func(cmd p.Command, cells []p.Pos, value float64, cost, towers int, exclusive string) {
		if ctx.Err() != nil {
			return
		}
		if cmd.Action == "build" && !r.Daylight() {
			return
		}
		if cost > gold {
			return
		}
		gs := g.Around(cells)
		if cmd.Action == "build" {
			filtered := []int{}
			for _, id := range gs {
				if g.Pos(id) != cmd.Targets[0] {
					filtered = append(filtered, id)
				}
			}
			gs = filtered
		}
		path := g.Shortest(g.ID(u.Pos), gs)
		if len(path) == 0 {
			return
		}
		d := len(path) - 1
		if len(defenseCells(r)) > 0 && (!r.Daylight() || d+1+returnDistance(r, g, g.Pos(path[len(path)-1]))+c.ReturnBuffer >= r.DayLeft()) {
			return
		}
		key := fmt.Sprintf("%s:%s:%v:%s", cmd.Action, cmd.Name, cells, exclusive)
		if key == previous {
			value *= 1.1
		}
		choices = append(choices, workOption{key: key, command: cmd, goals: gs, distance: d, gold: cost, towers: towers, value: value, exclusive: exclusive})
	}
	f := profile(r, c)
	if u.Health < 100 && u.Count("Medicine") > 0 {
		key := "use:Medicine:self"
		choices = append(choices, workOption{key: key, command: p.Command{Action: "use", Name: "Medicine"}, goals: []int{g.ID(u.Pos)}, value: 1000})
	}
	for _, target := range r.Our.Roles {
		if target.Type == "wall" && target.Health > 0 && target.Health < 400 && u.Count("WallFixer") > 0 {
			cmd := p.At("use", target.Pos)
			cmd.Name = "WallFixer"
			add(cmd, []p.Pos{target.Pos}, 80, 0, 0, "repair:"+strconv.Itoa(target.ID))
		}
	}
	if len(r.Weapons()) < 3 {
		for _, site := range f.Weapons {
			if ctx.Err() != nil {
				break
			}
			if !g.Free(site.Pos) || !buildConnected(r, g, site.Pos) {
				continue
			}
			cmd := p.At("build", site.Pos)
			cmd.Name = site.Kind
			add(cmd, []p.Pos{site.Pos}, 100, 25, 1, "build:"+siteKey(site.Pos))
		}
	}
	for _, target := range r.Our.Roles {
		if ctx.Err() != nil {
			break
		}
		if target.Health <= 0 || target.Level < 1 || target.Level >= 3 {
			continue
		}
		prefix := ""
		if target.Type == "station" {
			prefix = "StationUpgradeVoucher"
		} else if target.Weapon() {
			prefix = "WeaponUpgradeVoucher"
		}
		if prefix == "" {
			continue
		}
		name := prefix + strconv.Itoa(target.Level)
		if u.Count(name) > 0 {
			cmd := p.At("use", target.Pos)
			cmd.Name = name
			add(cmd, target.Cells(), 80, 0, 0, "upgrade:"+strconv.Itoa(target.ID))
		}
		for _, item := range r.Shop {
			if item.Name == name && item.Price > 0 && u.Count(name) == 0 && !u.Full() && gold-item.Price >= c.ReserveGold {
				add(p.Command{Action: "buy", Name: name, Num: 1}, zones(r, "weaponShop"), 55, item.Price, 0, "upgrade:"+strconv.Itoa(target.ID))
			}
		}
	}
	if u.Count("stone") > 0 {
		for _, q := range f.Walls {
			if ctx.Err() != nil {
				break
			}
			if g.Free(q) && q != u.Pos && buildConnected(r, g, q) {
				cmd := p.At("build", q)
				cmd.Name = "wall"
				add(cmd, []p.Pos{q}, 40, 0, 0, "build:"+siteKey(q))
			}
		}
	}
	for _, kind := range []string{"copper", "iron", "stone"} {
		n := u.Count(kind)
		if kind == "stone" && len(f.Walls) > 0 {
			n = max(0, n-2)
		}
		if n > 0 && (n >= c.MineBatch || u.Full() || near(u.Pos, zones(r, "vendor"))) {
			price := 1
			for _, item := range r.Vendor {
				if item.Name == kind {
					price = item.Price
				}
			}
			add(p.Command{Action: "sell", Name: kind, Num: n}, zones(r, "vendor"), float64(30+n*price), 0, 0, "")
		}
	}
	if !u.Full() {
		for _, z := range r.Map.Zones {
			if ctx.Err() != nil {
				break
			}
			if z.Type != "stone" && z.Type != "iron" && z.Type != "copper" {
				continue
			}
			price := 1
			for _, item := range r.Vendor {
				if item.Name == z.Type {
					price = item.Price
				}
			}
			if z.Type == "stone" && len(f.Walls) > 0 && u.Count("stone") < 3 {
				price += 5
			}
			add(p.At("collect", z.Pos), []p.Pos{z.Pos}, float64(10*price), 0, 0, "")
		}
	}
	sort.SliceStable(choices, func(i, j int) bool {
		return choices[i].value/float64(choices[i].distance+1) > choices[j].value/float64(choices[j].distance+1)
	})
	// Five work choices plus wait. Retain a still-valid incumbent among the five.
	if len(choices) > 5 {
		for i := 5; i < len(choices); i++ {
			if choices[i].key == previous {
				choices[4] = choices[i]
				break
			}
		}
		choices = choices[:5]
	}
	return append(choices, workOption{key: "wait", goals: []int{g.ID(u.Pos)}})
}

func assignWorkers(ctx context.Context, r p.Request, g nav.Grid, c Config, m *Memory, out *p.Response, goals map[int][]int, gold *int, tr *Trace) {
	roles := r.Mobiles()
	if len(roles) > 3 || baseEmergency(r) {
		return
	}
	var workers []int
	var options [][]workOption
	for i, u := range roles {
		if u.Type != "worker" {
			continue
		}
		if _, busy := out.Commands[strconv.Itoa(u.ID)]; busy {
			continue
		}
		if _, busy := goals[u.ID]; busy {
			continue
		}
		workers = append(workers, i)
		options = append(options, workerOptions(ctx, r, u, g, c, *gold, m.Work[u.ID]))
	}
	if len(workers) == 0 {
		return
	}
	wtr := &WorkTrace{Assignments: map[int]string{}, Proven: true, Reason: "best_within_candidates"}
	tr.Team = wtr
	if m.Work == nil {
		m.Work = map[int]string{}
	}
	type combo struct {
		picks []workOption
		bound float64
	}
	var combinations []combo
	picks := make([]workOption, len(workers))
	existingClaims := map[string]bool{}
	for _, cmd := range out.Commands {
		if len(cmd.Targets) != 1 {
			continue
		}
		if cmd.Action == "build" {
			existingClaims["build:"+siteKey(cmd.Targets[0])] = true
		}
		if cmd.Action == "use" {
			for _, u := range r.Our.Roles {
				if u.Pos == cmd.Targets[0] {
					if cmd.Name == "WallFixer" {
						existingClaims["repair:"+strconv.Itoa(u.ID)] = true
					} else if strings.Contains(cmd.Name, "UpgradeVoucher") {
						existingClaims["upgrade:"+strconv.Itoa(u.ID)] = true
					}
				}
			}
		}
	}
	var enumerate func(int)
	enumerate = func(i int) {
		if i < len(workers) {
			for _, o := range options[i] {
				picks[i] = o
				enumerate(i + 1)
			}
			return
		}
		cost, towers := 0, 0
		used := map[string]bool{}
		for key := range existingClaims {
			used[key] = true
		}
		bound := 0.0
		for _, o := range picks {
			cost += o.gold
			towers += o.towers
			if o.exclusive != "" {
				if used[o.exclusive] {
					return
				}
				used[o.exclusive] = true
			}
			bound += o.value / float64(o.distance+1)
		}
		if cost > *gold || towers+len(r.Weapons()) > 3 {
			return
		}
		combinations = append(combinations, combo{append([]workOption(nil), picks...), bound})
	}
	enumerate(0)
	sort.SliceStable(combinations, func(i, j int) bool { return combinations[i].bound > combinations[j].bound })
	priority := taskPriority(r, *m, roles, goals, *out)
	bestValue := -1.0
	bestArrival := int(^uint(0) >> 1)
	var winner []workOption
	remaining := max(100, c.MaxExpanded/3)
	for _, comb := range combinations {
		if priority == 0 && comb.bound < bestValue {
			continue
		}
		if remaining <= 0 || ctx.Err() != nil {
			wtr.Proven = false
			wtr.Reason = "search_budget"
			break
		}
		start := nav.State{}
		gs := make([][]int, len(roles))
		locked := [3]bool{}
		for i, u := range roles {
			start[i] = g.ID(u.Pos)
			gs[i] = goals[u.ID]
			if len(gs[i]) == 0 {
				gs[i] = []int{start[i]}
			}
			if _, busy := out.Commands[strconv.Itoa(u.ID)]; busy {
				locked[i] = true
				gs[i] = []int{start[i]}
			}
			for _, cmd := range out.Commands {
				if cmd.Controller == strconv.Itoa(u.ID) {
					locked[i] = true
					gs[i] = []int{start[i]}
				}
			}
			if u.Type == "pioneer" && r.PhaseTask != "" && !m.Task.Abandon {
				locked[i] = true
				gs[i] = []int{start[i]}
			}
		}
		for i, wi := range workers {
			gs[wi] = comb.picks[i].goals
			locked[wi] = comb.picks[i].key == "wait" || comb.picks[i].distance == 0
		}
		// Reserve planned construction cells jointly, not only one at a time.
		grid := g.Clone()
		compatible := true
		for _, o := range comb.picks {
			if o.command.Action == "build" {
				q := o.command.Targets[0]
				if !buildConnected(r, grid, q) {
					compatible = false
					break
				}
				grid.Block[grid.ID(q)] = true
			}
		}
		if !compatible {
			continue
		}
		opt := nav.Options{Follow: c.FollowMoves, Locked: locked, MaxExpanded: min(remaining, max(50, c.MaxExpanded/36)), MaxStates: c.MaxStates, Priority: priority}
		if priority > 0 {
			opt.MaxRounds = max(1, r.DayLeft()-c.ReturnBuffer-1)
		}
		res := nav.Joint(ctx, grid, start, gs, opt)
		wtr.Evaluated++
		wtr.Expanded += res.Expanded
		remaining -= max(1, res.Expanded)
		if !res.Optimal {
			wtr.Proven = false
			wtr.Reason = "incomplete_candidate_search"
		}
		if res.Cost < 0 {
			continue
		}
		// Independent distances used for candidate generation are optimistic;
		// recheck the actual joint completion and return route before admission.
		safe := true
		for i, wi := range workers {
			if comb.picks[i].key == "wait" {
				continue
			}
			at := grid.Pos(res.Path[len(res.Path)-1][wi])
			if len(defenseCells(r)) > 0 && res.Cost+1+returnDistance(r, grid, at)+c.ReturnBuffer >= r.DayLeft() {
				safe = false
			}
		}
		if !safe {
			wtr.Proven = false
			wtr.Reason = "return_deadline_rejected"
			continue
		}
		value := 0.0
		for _, o := range comb.picks {
			value += o.value / float64(res.Cost+1)
		}
		if res.PriorityArrival < bestArrival || res.PriorityArrival == bestArrival && value > bestValue {
			winner = comb.picks
			bestArrival = res.PriorityArrival
			bestValue = value
		}
	}
	if winner == nil {
		wtr.Proven = false
		wtr.Reason = "no_verified_assignment"
		return
	}
	for i, wi := range workers {
		u := roles[wi]
		o := winner[i]
		m.Work[u.ID] = o.key
		wtr.Assignments[u.ID] = o.key
		goals[u.ID] = o.goals
		if o.key == "wait" {
			continue
		}
		if o.distance == 0 {
			out.Commands[strconv.Itoa(u.ID)] = o.command
			*gold -= o.gold
		}
	}
}
