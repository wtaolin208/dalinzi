package game

import (
	p "competition/internal/protocol"
	"math"
	"sort"
	"strconv"
)

type Pair struct {
	Role   p.Role `json:"role"`
	Weapon p.Role `json:"weapon"`
}
type Shot struct {
	Targets []p.Pos `json:"targets"`
	Damage  []int   `json:"damage"`
	Value   float64 `json:"value"`
}

// Ray intersects closed cell squares. Corner ties are ordered by robot ID.
// This explicit geometric hypothesis needs calibration against the official judge.
func rayEntry(a, b, q p.Pos) (float64, bool) {
	lo, hi := 0.0, 1.0
	for _, v := range [][3]float64{{float64(a.X), float64(b.X - a.X), float64(q.X)}, {float64(a.Y), float64(b.Y - a.Y), float64(q.Y)}} {
		if v[1] == 0 {
			if math.Abs(v[0]-v[2]) > 0.5 {
				return 0, false
			}
			continue
		}
		x, y := (v[2]-0.5-v[0])/v[1], (v[2]+0.5-v[0])/v[1]
		if x > y {
			x, y = y, x
		}
		lo = math.Max(lo, x)
		hi = math.Min(hi, y)
		if lo > hi {
			return 0, false
		}
	}
	return lo, true
}
func cone(origin p.Pos, targets []p.Pos) bool {
	for i, a := range targets {
		if a == origin {
			return false
		}
		for _, b := range targets[:i] {
			if (a.X-origin.X)*(b.X-origin.X)+(a.Y-origin.Y)*(b.Y-origin.Y) < 0 {
				return false
			}
		}
	}
	return true
}
func shotDamage(r p.Request, w p.Role, targets []p.Pos) []int {
	d := make([]int, len(r.Robots.Roles))
	for _, q := range targets {
		if w.Type == "rocket" {
			for i, u := range r.Robots.Roles {
				if u.Health <= 0 {
					continue
				}
				switch p.Distance(q, u.Pos) {
				case 0:
					d[i] += 20
				case 1:
					d[i] += 10
				}
			}
			continue
		}
		type hit struct {
			i int
			t float64
		}
		var hits []hit
		for i, u := range r.Robots.Roles {
			if u.Health > 0 {
				if t, ok := rayEntry(w.Pos, q, u.Pos); ok {
					hits = append(hits, hit{i, t})
				}
			}
		}
		sort.Slice(hits, func(i, j int) bool {
			if hits[i].t != hits[j].t {
				return hits[i].t < hits[j].t
			}
			return r.Robots.Roles[hits[i].i].ID < r.Robots.Roles[hits[j].i].ID
		})
		if w.Type == "gatling" {
			if len(hits) > 0 {
				d[hits[0].i] += 10
			}
		} else {
			energy := 10 * min(3, max(1, w.Level))
			for _, h := range hits {
				n := min(energy, r.Robots.Roles[h.i].Health)
				d[h.i] += n
				energy -= n
				if energy <= 0 {
					break
				}
			}
		}
	}
	return d
}
func damageValue(r p.Request, d []int) float64 {
	v := 0.0
	for i, u := range r.Robots.Roles {
		if u.Health <= 0 {
			continue
		}
		if !ownThreat(r, u.Target) {
			continue
		}
		threat := 1.0
		for _, a := range r.Our.Roles {
			if a.Health > 0 && (a.Type == "station" || a.Mobile()) {
				dist := p.Distance(a.Pos, u.Pos)
				if dist <= 5 {
					threat += float64(6-dist) * 0.8
				}
			}
		}
		weight := 1.0
		if !r.Daylight() && d[i] < u.Health {
			// No damage points are awarded. Unfinished targets disappear at dawn;
			// on the final night turn partial damage has no future scoring value.
			left := 130 - (r.Round-1)%130
			weight = min(1, float64(left-1)/5)
		}
		points := map[string]int{"smallRobot": 1, "middleRobot": 2, "largeRobot": 4, "bossRobot": 10}[u.Type]
		// Planning value only: partial damage earns no actual score.
		v += float64(points) * 12 * float64(min(u.Health, d[i])) / float64(u.Health) * threat * weight
		for _, a := range r.Our.Roles {
			if a.Health <= 0 || a.Mobile() {
				continue
			}
			dist := 10000
			for _, cell := range a.Cells() {
				dist = min(dist, p.Distance(cell, u.Pos))
			}
			if dist <= 4 && a.Health <= robotPower(u.Type)*3 {
				v += float64(min(u.Health, d[i])) * 1000
			}
		}
		if d[i] >= u.Health {
			v += float64(points) * 12 * threat
		}
	}
	return v
}
func candidates(r p.Request, w p.Role, c Config) []Shot {
	var points []p.Pos
	seen := map[p.Pos]bool{}
	for _, u := range r.Robots.Roles {
		if u.Health <= 0 {
			continue
		}
		radius := 0
		if w.Type == "rocket" {
			radius = 1
		}
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				q := p.Pos{X: u.Pos.X + dx, Y: u.Pos.Y + dy}
				if r.In(q) && q != w.Pos && p.Distance(w.Pos, q) <= w.Range() && !seen[q] {
					seen[q] = true
					points = append(points, q)
				}
			}
		}
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].Y != points[j].Y {
			return points[i].Y < points[j].Y
		}
		return points[i].X < points[j].X
	})
	// Pre-rank single impacts; all pruning belongs to combat, never the exact navigator.
	singles := make([]Shot, 0, len(points))
	for _, q := range points {
		d := shotDamage(r, w, []p.Pos{q})
		singles = append(singles, Shot{[]p.Pos{q}, d, damageValue(r, d)})
	}
	sort.SliceStable(singles, func(i, j int) bool { return singles[i].Value > singles[j].Value })
	if len(singles) > c.CombatCandidates {
		singles = singles[:c.CombatCandidates]
	}
	count := 1
	if w.Type != "railgun" {
		count = min(3, max(1, w.Level))
	}
	beam := []Shot{{Damage: make([]int, len(r.Robots.Roles))}}
	for k := 0; k < count; k++ {
		var next []Shot
		for _, s := range beam {
			for _, one := range singles {
				q := one.Targets[0]
				dup := false
				for _, prev := range s.Targets {
					if prev == q {
						dup = true
					}
				}
				if dup && !c.AllowDuplicateShots {
					continue
				}
				targets := append(append([]p.Pos(nil), s.Targets...), q)
				if w.Type == "gatling" && !cone(w.Pos, targets) {
					continue
				}
				d := append([]int(nil), s.Damage...)
				for i := range d {
					d[i] += one.Damage[i]
				}
				next = append(next, Shot{targets, d, damageValue(r, d)})
			}
		}
		sort.SliceStable(next, func(i, j int) bool { return next[i].Value > next[j].Value })
		if len(next) > c.CombatCandidates {
			next = next[:c.CombatCandidates]
		}
		beam = next
	}
	return beam
}
func fight(r p.Request, pairs []Pair, c Config, m Memory, out *p.Response, tr *Trace) {
	pairs = append([]Pair(nil), pairs...)
	order := map[string]int{}
	for i, k := range c.Strategy.FireOrder {
		order[k] = i
	}
	sort.SliceStable(pairs, func(i, j int) bool { return order[pairs[i].Weapon.Type] < order[pairs[j].Weapon.Type] })
	type option struct {
		commands map[string]p.Command
		damage   []int
		value    float64
	}
	initial := make([]int, len(r.Robots.Roles))
	copy(initial, tr.ItemDamage)
	beam := []option{{map[string]p.Command{}, initial, damageValue(r, initial)}}
	combinations := 1
	for range pairs {
		combinations *= c.CombatCandidates + 1
	}
	exact := combinations <= 4096
	tr.CombatExact = exact
	for _, pair := range pairs {
		w, u := pair.Weapon, pair.Role
		if p.Distance(w.Pos, u.Pos) > 1 {
			continue
		}
		if _, busy := out.Commands[strconv.Itoa(u.ID)]; busy {
			continue
		}
		if w.Cooldown != nil && *w.Cooldown > 0 || w.Cooldown == nil && r.Round < m.RocketNext[w.ID] {
			continue
		}
		shots := candidates(r, w, c)
		var next []option
		for _, b := range beam {
			next = append(next, b)
			for _, s := range shots {
				cmds := map[string]p.Command{}
				for k, v := range b.commands {
					cmds[k] = v
				}
				cmds[strconv.Itoa(w.ID)] = p.Command{Action: "attack", Controller: strconv.Itoa(u.ID), Targets: s.Targets}
				d := append([]int(nil), b.damage...)
				for i := range d {
					d[i] += s.Damage[i]
				}
				next = append(next, option{cmds, d, damageValue(r, d)})
			}
		}
		sort.SliceStable(next, func(i, j int) bool { return next[i].value > next[j].value })
		if !exact && len(next) > c.CombatBeam {
			next = next[:c.CombatBeam]
		}
		beam = next
	}
	if len(beam) > 0 {
		for k, v := range beam[0].commands {
			out.Commands[k] = v
		}
		tr.PredictedDamage = beam[0].damage
		tr.CombatValue = beam[0].value
	}
}
