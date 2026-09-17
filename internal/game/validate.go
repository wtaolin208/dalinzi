package game

import (
	p "competition/internal/protocol"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func Validate(r p.Request, c Config, input p.Response, tr *Trace) p.Response {
	out := p.Empty()
	out.Prompt = input.Prompt
	out.Execute = input.Execute
	if r.PhaseTask == "" {
		out.Execute = ""
	}
	if len(out.Execute) > c.MaxToolBytes {
		out.Execute = ""
	}
	units := map[int]p.Role{}
	for _, u := range r.Our.Roles {
		units[u.ID] = u
	}
	keys := make([]string, 0, len(input.Commands))
	for k := range input.Commands {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, _ := strconv.Atoi(keys[i])
		b, _ := strconv.Atoi(keys[j])
		if a == b {
			return keys[i] < keys[j]
		}
		return a < b
	})
	g := staticGrid(r)
	gold := r.Our.Gold
	towers := len(r.Weapons())
	built := map[p.Pos]bool{}
	controllers := map[int]bool{}
	usedItems := map[int]map[string]int{}
	f := profile(r, c)
	reject := func(key, reason string) {
		if tr.Rejected == nil {
			tr.Rejected = map[string]string{}
		}
		tr.Rejected[key] = reason
	}
	for _, key := range keys {
		cmd := input.Commands[key]
		id, err := strconv.Atoi(key)
		u, exists := units[id]
		reason := ""
		if err != nil || strconv.Itoa(id) != key || !exists || u.Health <= 0 {
			reject(key, "unknown/dead unit or noncanonical key")
			continue
		}
		one := len(cmd.Targets) == 1
		in := true
		for _, q := range cmd.Targets {
			if !r.In(q) {
				in = false
			}
		}
		if !in {
			reject(key, "target outside map")
			continue
		}
		adj := one && p.Distance(u.Pos, cmd.Targets[0]) <= 1
		take := func(name string, n int) bool {
			if usedItems[id] == nil {
				usedItems[id] = map[string]int{}
			}
			if n < 1 || u.Count(name)-usedItems[id][name] < n {
				return false
			}
			usedItems[id][name] += n
			return true
		}
		switch cmd.Action {
		case "move":
			if !u.Mobile() || !adj || cmd.Targets[0] == u.Pos || !g.Free(cmd.Targets[0]) {
				reason = "invalid move"
			}
		case "attack":
			ctrl, e := strconv.Atoi(cmd.Controller)
			actor, ok := units[ctrl]
			count := 1
			if u.Type != "railgun" {
				count = min(3, max(1, u.Level))
			}
			if !u.Weapon() || r.Daylight() || e != nil || strconv.Itoa(ctrl) != cmd.Controller || !ok || !actor.Mobile() || actor.Health <= 0 || p.Distance(u.Pos, actor.Pos) > 1 || controllers[ctrl] || len(cmd.Targets) != count || u.Cooldown != nil && *u.Cooldown > 0 {
				reason = "attack/controller/count/cooldown constraint"
				break
			}
			if _, busy := input.Commands[cmd.Controller]; busy {
				reason = "controller already has an action"
				break
			}
			for _, q := range cmd.Targets {
				if q == u.Pos || p.Distance(u.Pos, q) > u.Range() {
					reason = "attack range"
				}
			}
			if u.Type == "gatling" && !cone(u.Pos, cmd.Targets) {
				reason = "gatling cone"
			}
			if !c.AllowDuplicateShots {
				seen := map[p.Pos]bool{}
				for _, q := range cmd.Targets {
					if seen[q] {
						reason = "duplicate shots disabled until verified"
					}
					seen[q] = true
				}
			}
			if reason == "" {
				controllers[ctrl] = true
			}
		case "build":
			if u.Type != "worker" || !r.Daylight() || !adj || !g.Free(cmd.Targets[0]) || built[cmd.Targets[0]] {
				reason = "build phase/range/occupancy"
				break
			}
			q := cmd.Targets[0]
			occupied := false
			for _, a := range r.Mobiles() {
				if a.Pos == q {
					occupied = true
				}
			}
			if occupied {
				reason = "build occupied by role"
				break
			}
			legal := false
			if cmd.Name == "wall" {
				for _, s := range f.Walls {
					if s == q {
						legal = true
					}
				}
				if !legal || !take("stone", 1) || !buildConnected(r, g, q) {
					reason = "wall zone/material/connectivity"
				}
			} else {
				for _, s := range f.Weapons {
					if s.Pos == q && s.Kind == cmd.Name {
						legal = true
					}
				}
				if !legal || gold < 25 || towers >= 3 || !buildConnected(r, g, q) {
					reason = "weapon zone/gold/count/connectivity"
				} else {
					gold -= 25
					towers++
				}
			}
			if reason == "" {
				built[q] = true
				g.Block[g.ID(q)] = true
			}
		case "collect":
			if u.Type != "worker" || !adj || u.Full() {
				reason = "collect role/range/capacity"
				break
			}
			found := false
			for _, z := range r.Map.Zones {
				if z.Pos == cmd.Targets[0] && (z.Type == "stone" || z.Type == "iron" || z.Type == "copper") {
					found = true
				}
			}
			if !found {
				reason = "not a mine"
			}
		case "buy", "sell":
			shop := r.Shop
			cells := zones(r, "weaponShop")
			if cmd.Action == "sell" {
				shop = r.Vendor
				cells = zones(r, "vendor")
			}
			if !u.Mobile() || !near(u.Pos, cells) || cmd.Name == "" || cmd.Num < 1 {
				reason = "trade role/range/name/count"
				break
			}
			price := -1
			for _, item := range shop {
				if item.Name == cmd.Name {
					price = item.Price
				}
			}
			if price < 0 {
				reason = "unknown shop item"
				break
			}
			if cmd.Action == "sell" {
				if !take(cmd.Name, cmd.Num) {
					reason = "insufficient ore"
				}
			} else {
				cap := 100
				if u.Type == "pioneer" {
					cap = 40
				}
				if u.Capacity != nil {
					cap = *u.Capacity
				}
				if cmd.Num > cap-len(u.Backpack) || price > 0 && cmd.Num > gold/price {
					reason = "buy capacity/gold"
				} else {
					gold -= cmd.Num * price
				}
			}
		case "acceptTask":
			valid := false
			for _, site := range r.Our.Tasks {
				if site.Valid && site.Cooldown == 0 && near(u.Pos, taskCells(r, site)) {
					valid = true
				}
			}
			if u.Type != "pioneer" || r.PhaseTask != "" || !valid {
				reason = "task role/state/point"
			}
		case "submitAnswer":
			if u.Type != "pioneer" || r.PhaseTask == "" || cmd.Answer == nil || strings.TrimSpace(*cmd.Answer) == "" {
				reason = "answer missing or no active task"
			}
		case "summonTreasure":
			if u.Type != "pioneer" || !adj || len(cmd.Item) == 0 {
				reason = "treasure role/range/items"
				break
			}
			for _, name := range cmd.Item {
				if !take(name, 1) {
					reason = "treasure items absent"
				}
			}
		case "drop":
			if !u.Mobile() || !take(cmd.Name, 1) {
				reason = "drop missing item"
			}
		case "remove":
			valid := false
			if adj {
				for _, a := range r.Our.Roles {
					if a.Type == "wall" && a.Health > 0 && a.Pos == cmd.Targets[0] {
						valid = true
					}
				}
			}
			if u.Type != "worker" || !valid {
				reason = "remove not adjacent own wall"
			}
		case "use":
			if !u.Mobile() || !take(cmd.Name, 1) {
				reason = "use item missing"
				break
			}
			switch cmd.Name {
			case "Medicine":
			case "Bomb", "DizzyWeapon":
				if !one {
					reason = "area item target required"
				}
			case "SmallRobotSummonOrder", "MiddleRobotSummonOrder", "LargeRobotSummonOrder", "BossRobotSummonOrder":
				// Timing and strategic desirability are planner gates, not field legality.
			default:
				found := false
				if one {
					for _, a := range r.Our.Roles {
						if a.Pos != cmd.Targets[0] || a.Health <= 0 || !near(u.Pos, a.Cells()) {
							continue
						}
						expected := ""
						if cmd.Name == "WallFixer" && a.Type == "wall" {
							found = true
						}
						if a.Type == "station" {
							expected = "StationUpgradeVoucher"
						} else if a.Weapon() {
							expected = "WeaponUpgradeVoucher"
						} else if a.Type == "wall" {
							expected = "WallUpgradeVoucher"
						}
						if expected != "" && a.Level >= 1 && a.Level < 3 && cmd.Name == expected+strconv.Itoa(a.Level) {
							found = true
						}
					}
				}
				if !found {
					reason = "unknown item or upgrade target/level/range"
				}
			}
		default:
			reason = "unknown action"
		}
		if reason != "" {
			reject(key, reason)
		} else {
			out.Commands[key] = cmd
		}
	}
	// Removing a conflicting move may block a following move: iterate to a fixed point.
	for changed := true; changed; {
		changed = false
		dest := map[p.Pos][]int{}
		roles := r.Mobiles()
		for _, u := range roles {
			q := u.Pos
			if cmd, ok := out.Commands[strconv.Itoa(u.ID)]; ok && cmd.Action == "move" {
				q = cmd.Targets[0]
			}
			dest[q] = append(dest[q], u.ID)
		}
		bad := map[int]string{}
		for _, u := range roles {
			cmd, ok := out.Commands[strconv.Itoa(u.ID)]
			if !ok || cmd.Action != "move" {
				continue
			}
			q := cmd.Targets[0]
			if built[q] {
				bad[u.ID] = "move into new building"
			}
			if len(dest[q]) > 1 {
				bad[u.ID] = "friendly destination conflict"
			}
			for _, v := range roles {
				if v.ID == u.ID || q != v.Pos {
					continue
				}
				other, moving := out.Commands[strconv.Itoa(v.ID)]
				if !c.FollowMoves || !moving || other.Action != "move" || other.Targets[0] == u.Pos {
					bad[u.ID] = "friendly occupied/swap conflict"
				}
			}
		}
		for id, reason := range bad {
			key := strconv.Itoa(id)
			delete(out.Commands, key)
			reject(key, reason)
			changed = true
		}
	}
	return out
}
func ExplainErrors(r p.Request) []string {
	a := []string{}
	for _, e := range r.Errors {
		a = append(a, fmt.Sprintf("%d: %s", e.Code, strings.TrimSpace(e.Description)))
	}
	return a
}
