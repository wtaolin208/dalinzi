package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
	"context"
	"fmt"
	"sort"
	"strconv"
)

type PathTrace struct {
	Roles      []int      `json:"roles"`
	Goals      [][]int    `json:"goals"`
	Result     nav.Result `json:"result"`
	Assignment []int      `json:"assignment,omitempty"`
}
type Trace struct {
	Mode            string              `json:"mode"`
	Notes           []string            `json:"notes"`
	Paths           []PathTrace         `json:"paths"`
	Rejected        map[string]string   `json:"rejected"`
	PredictedDamage []int               `json:"predictedDamage,omitempty"`
	CombatValue     float64             `json:"combatValue"`
	MapHypothesis   bool                `json:"mapHypothesis"`
	Movement        []MoveAssessment    `json:"movement,omitempty"`
	Tasks           []TaskAssessment    `json:"tasks,omitempty"`
	Learning        *LearningAssessment `json:"learning,omitempty"`
	Team            *WorkTrace          `json:"team,omitempty"`
}
type Engine struct{ Config Config }

func (e Engine) Decide(ctx context.Context, r p.Request, before Memory) (p.Response, Memory, Trace) {
	m := CloneMemory(before)
	tr := Trace{Rejected: map[string]string{}, MapHypothesis: e.Config.ExperimentalDemoLayout}
	updateMemory(r, &m, &tr)
	out := p.Empty()
	g := staticGrid(r)
	roles := r.Mobiles()
	goals := map[int][]int{}
	emergency := baseEmergency(r)
	if r.PhaseTask != "" && shouldRetreat(r, g, e.Config, m, emergency) {
		m.Task.Abandon = true
		m.Task.Pending = Pending{}
		m.Task.Candidate = nil
		tr.Notes = append(tr.Notes, "task abandoned for defense: leaving task area")
	}
	planning := r
	if m.Task.Abandon {
		planning.PhaseTask = ""
	}
	pairs, paths := defense(ctx, planning, g, e.Config)
	tr.Paths = append(tr.Paths, paths...)
	returnTime := 0
	for _, pair := range pairs {
		d := g.Distances(g.Around([]p.Pos{pair.Weapon.Pos}))
		if d[g.ID(pair.Role.Pos)] >= 0 {
			returnTime = max(returnTime, d[g.ID(pair.Role.Pos)])
		}
	}
	defending := emergency || m.Task.Abandon || !r.Daylight() || r.DayLeft() <= returnTime+e.Config.ReturnBuffer
	tr.Mode = "economy"
	if defending {
		tr.Mode = "defense"
	}
	if len(profile(r, e.Config).Weapons) == 0 {
		tr.Notes = append(tr.Notes, "no verified build profile: building disabled; supply profiles or explicitly opt into experimental demo layout")
	}
	for _, u := range roles {
		if u.Type == "pioneer" && r.PhaseTask != "" && !m.Task.Abandon {
			if taskTurn(r, u, e.Config, &m, &out, &tr) {
				goals[u.ID] = []int{g.ID(u.Pos)}
			}
		}
		if u.Type == "pioneer" && m.Task.Abandon {
			goals[u.ID] = retreatGoals(r, g, m.Task.Position)
		}
	}
	if !r.Daylight() {
		combatPairs := make([]Pair, 0, len(pairs))
		for _, pair := range pairs {
			if !m.Task.Abandon || pair.Role.Type != "pioneer" {
				combatPairs = append(combatPairs, pair)
			}
		}
		fight(r, combatPairs, e.Config, m, &out, &tr)
	}
	gold := r.Our.Gold
	for _, u := range roles {
		key := strconv.Itoa(u.ID)
		if _, ok := out.Commands[key]; ok {
			continue
		}
		controlling := false
		for _, cmd := range out.Commands {
			if cmd.Controller == key {
				controlling = true
			}
		}
		if controlling {
			goals[u.ID] = []int{g.ID(u.Pos)}
			continue
		}
		if _, locked := goals[u.ID]; locked {
			continue
		}
		if (u.Type != "worker" || defending) && useOwned(r, u, &out) {
			goals[u.ID] = []int{g.ID(u.Pos)}
			continue
		}
		if defending {
			for _, pair := range pairs {
				if pair.Role.ID == u.ID {
					goals[u.ID] = g.Around([]p.Pos{pair.Weapon.Pos})
				}
			}
			if _, ok := goals[u.ID]; ok {
				continue
			}
		}
		if u.Type == "pioneer" && e.Config.EnableTasks {
			if pioneer(r, u, g, e.Config, &m, &out, goals, &gold, &tr) {
				continue
			}
		}
	}
	assignWorkers(ctx, r, g, e.Config, &m, &out, goals, &gold, &tr)
	// New buildings already reserved this turn must also constrain every route.
	for _, cmd := range out.Commands {
		if cmd.Action == "build" && len(cmd.Targets) == 1 {
			q := cmd.Targets[0]
			if g.In(q) {
				g.Block[g.ID(q)] = true
			}
		}
	}
	// Every nonmoving or acting role is included with a singleton goal: no collision with an idle teammate.
	if len(roles) > 0 {
		start := nav.State{}
		gs := make([][]int, len(roles))
		ids := make([]int, len(roles))
		locked := [3]bool{}
		moving := false
		for i, u := range roles {
			ids[i] = u.ID
			start[i] = g.ID(u.Pos)
			gs[i] = goals[u.ID]
			if len(gs[i]) == 0 {
				gs[i] = []int{start[i]}
			}
			if _, busy := out.Commands[strconv.Itoa(u.ID)]; busy {
				gs[i] = []int{start[i]}
				locked[i] = true
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
			for _, q := range gs[i] {
				if q != start[i] {
					moving = true
				}
			}
		}
		if moving {
			priority := taskPriority(r, m, roles, goals, out)
			opt := nav.Options{MaxExpanded: e.Config.MaxExpanded, MaxStates: e.Config.MaxStates, Follow: e.Config.FollowMoves, Locked: locked, Priority: priority}
			if priority > 0 {
				opt.MaxRounds = max(1, r.DayLeft()-e.Config.ReturnBuffer-1)
			}
			res := nav.Joint(ctx, g, start, gs, opt)
			tr.Paths = append(tr.Paths, PathTrace{Roles: ids, Goals: gs, Result: res})
			if len(res.Path) > 1 {
				for i, u := range roles {
					if res.Path[1][i] != start[i] {
						out.Commands[strconv.Itoa(u.ID)] = p.At("move", g.Pos(res.Path[1][i]))
					}
				}
			} else if !res.Optimal {
				tr.Notes = append(tr.Notes, "joint search budget exhausted: deterministic collision-free one-step fallback, NOT proven optimal")
				fallback(g, start, gs, roles, e.Config.FollowMoves, &out)
			}
			assessMovement(r, g, start, gs, roles, locked, e.Config.FollowMoves, m, &out, &tr, priority)
		}
	}
	newsTurn(r, e.Config, &m, &out)
	out = Validate(r, e.Config, out, &tr)
	// Only accepted local actions affect predictive memory; judge snapshot remains authoritative.
	for _, w := range r.Weapons() {
		if cmd, ok := out.Commands[strconv.Itoa(w.ID)]; ok && cmd.Action == "attack" && w.Type == "rocket" {
			m.RocketNext[w.ID] = r.Round + 4
		}
	}
	m.LastPositions = map[int]p.Pos{}
	for _, u := range roles {
		m.LastPositions[u.ID] = u.Pos
	}
	m.Round = r.Round
	m.LastResponse = out
	return out, m, tr
}
func defense(ctx context.Context, r p.Request, g nav.Grid, c Config) ([]Pair, []PathTrace) {
	roles := r.Mobiles()
	ws := r.Weapons()
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
		if cost < bestCost {
			bestCost = cost
			best = append([]Pair(nil), chosen...)
		}
	}
	visit(0, 0)
	return best, traces
}
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
func useOwned(r p.Request, u p.Role, out *p.Response) bool {
	if u.Health < 100 && u.Count("Medicine") > 0 {
		out.Commands[strconv.Itoa(u.ID)] = p.Command{Action: "use", Name: "Medicine"}
		return true
	}
	for _, target := range r.Our.Roles {
		if target.Health <= 0 || !near(u.Pos, target.Cells()) {
			continue
		}
		name := ""
		switch target.Type {
		case "station":
			name = "StationUpgradeVoucher"
		case "gatling", "railgun", "rocket":
			name = "WeaponUpgradeVoucher"
		}
		if name != "" && target.Level >= 1 && target.Level < 3 {
			name += strconv.Itoa(target.Level)
			if u.Count(name) > 0 {
				cmd := p.At("use", target.Pos)
				cmd.Name = name
				out.Commands[strconv.Itoa(u.ID)] = cmd
				return true
			}
		}
		if target.Type == "wall" && target.Health < 400 && u.Count("WallFixer") > 0 {
			cmd := p.At("use", target.Pos)
			cmd.Name = "WallFixer"
			out.Commands[strconv.Itoa(u.ID)] = cmd
			return true
		}
	}
	return false
}
func pioneer(r p.Request, u p.Role, g nav.Grid, c Config, m *Memory, out *p.Response, goals map[int][]int, gold *int, traces ...*Trace) bool {
	if !r.Daylight() || baseEmergency(r) {
		return false
	}
	if !m.TreasureDone && m.Treasure != nil {
		t := m.Treasure
		if r.Round > t.Latest {
			m.Treasure = nil
		} else if r.Round >= t.Earliest-20 {
			need := map[string]int{}
			for _, s := range t.Items {
				need[s]++
			}
			names := make([]string, 0, len(need))
			for s := range need {
				names = append(names, s)
			}
			sort.Strings(names)
			for _, s := range names {
				if u.Count(s) < need[s] {
					for _, item := range r.Shop {
						if item.Name == s && item.Price > 0 && *gold >= item.Price && !u.Full() {
							cmd := p.Command{Action: "buy", Name: s, Num: 1}
							if moveOr(r, u, g, zones(r, "weaponShop"), cmd, out, goals) {
								if near(u.Pos, zones(r, "weaponShop")) {
									*gold -= item.Price
								}
								return true
							}
						}
					}
					return false
				}
			}
			if r.Round >= t.Earliest {
				cmd := p.At("summonTreasure", t.Target)
				cmd.Item = t.Items
				if moveOr(r, u, g, []p.Pos{t.Target}, cmd, out, goals) {
					if near(u.Pos, []p.Pos{t.Target}) {
						m.LastTreasureRound = r.Round
					}
					return true
				}
			}
		}
	}
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
		a := assessTask(r, g, c, *m, site, len(path)-1, g.Pos(path[len(path)-1]))
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
	if selected == nil {
		return false
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
func economy(r p.Request, u p.Role, g nav.Grid, c Config, out *p.Response, goals map[int][]int, gold *int, claimed map[p.Pos]bool, towers *int) {
	f := profile(r, c)
	if r.Daylight() && *towers < 3 && *gold >= 25 {
		for _, site := range f.Weapons {
			if claimed[site.Pos] || !g.Free(site.Pos) || !buildConnected(r, g, site.Pos) {
				continue
			}
			cmd := p.At("build", site.Pos)
			cmd.Name = site.Kind
			if moveOr(r, u, g, []p.Pos{site.Pos}, cmd, out, goals) {
				claimed[site.Pos] = true
				if near(u.Pos, []p.Pos{site.Pos}) {
					*gold -= 25
					*towers++
				}
				return
			}
		}
	}
	// Deliver owned vouchers before making another shop trip.
	for _, target := range r.Our.Roles {
		prefix := ""
		if target.Type == "station" {
			prefix = "StationUpgradeVoucher"
		} else if target.Weapon() {
			prefix = "WeaponUpgradeVoucher"
		}
		if prefix != "" && target.Health > 0 && target.Level >= 1 && target.Level < 3 {
			name := prefix + strconv.Itoa(target.Level)
			if u.Count(name) > 0 {
				cmd := p.At("use", target.Pos)
				cmd.Name = name
				if moveOr(r, u, g, target.Cells(), cmd, out, goals) {
					return
				}
			}
		}
	}
	if r.Daylight() && u.Count("stone") > 0 {
		for _, q := range f.Walls {
			if claimed[q] || !g.Free(q) || p.Distance(u.Pos, q) == 0 {
				continue
			}
			if !buildConnected(r, g, q) {
				continue
			}
			cmd := p.At("build", q)
			cmd.Name = "wall"
			if moveOr(r, u, g, []p.Pos{q}, cmd, out, goals) {
				claimed[q] = true
				return
			}
		}
	}
	// Sell highest-value held ore, keeping two stones for walls only when walls are configured.
	for _, s := range []string{"copper", "iron", "stone"} {
		n := u.Count(s)
		if s == "stone" && len(f.Walls) > 0 {
			n = max(0, n-2)
		}
		if n > 0 && (n >= c.MineBatch || u.Full() || near(u.Pos, zones(r, "vendor"))) {
			if moveOr(r, u, g, zones(r, "vendor"), p.Command{Action: "sell", Name: s, Num: n}, out, goals) {
				return
			}
		}
	}
	desired := ""
	for _, target := range r.Our.Roles {
		if target.Health > 0 && target.Type == "station" && target.Level >= 1 && target.Level < 3 {
			desired = "StationUpgradeVoucher" + strconv.Itoa(target.Level)
			break
		}
	}
	if desired == "" {
		for _, target := range r.Weapons() {
			if target.Level >= 1 && target.Level < 3 {
				desired = "WeaponUpgradeVoucher" + strconv.Itoa(target.Level)
				break
			}
		}
	}
	for _, item := range r.Shop {
		if item.Name == desired && u.Count(desired) == 0 && !u.Full() && item.Price > 0 && *gold-item.Price >= c.ReserveGold {
			if moveOr(r, u, g, zones(r, "weaponShop"), p.Command{Action: "buy", Name: desired, Num: 1}, out, goals) {
				if near(u.Pos, zones(r, "weaponShop")) {
					*gold -= item.Price
				}
				return
			}
		}
	}
	if u.Full() {
		return
	}
	best := -1.0
	var mine p.Pos
	for _, z := range r.Map.Zones {
		if z.Type != "stone" && z.Type != "iron" && z.Type != "copper" {
			continue
		}
		path := g.Shortest(g.ID(u.Pos), g.Around([]p.Pos{z.Pos}))
		if path == nil {
			continue
		}
		price := 1
		for _, v := range r.Vendor {
			if v.Name == z.Type {
				price = v.Price
			}
		}
		if z.Type == "stone" && len(f.Walls) > 0 && u.Count("stone") < 3 {
			price += 5
		}
		v := float64(price) / float64(len(path)+1)
		if v > best {
			best = v
			mine = z.Pos
		}
	}
	if best >= 0 {
		moveOr(r, u, g, []p.Pos{mine}, p.At("collect", mine), out, goals)
	}
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
func (t Trace) String() string {
	return fmt.Sprintf("mode=%s paths=%d rejected=%d", t.Mode, len(t.Paths), len(t.Rejected))
}
