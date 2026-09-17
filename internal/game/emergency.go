package game

import (
	p "competition/internal/protocol"
	"strconv"
)

// Reserve lifesaving role actions before selecting weapon controllers.
func emergencyItems(r p.Request, c Config, plan BattlePlan, pairs []Pair, out *p.Response, tr *Trace) {
	critical := map[int]bool{}
	for _, x := range plan.Risks {
		critical[x.ID] = x.Critical
	}
	tr.ItemDamage = make([]int, len(r.Robots.Roles))
	repaired := map[int]bool{}
	stunned := map[int]bool{}
	for _, u := range r.Mobiles() {
		if len(u.Backpack) == 0 {
			continue
		}
		key := strconv.Itoa(u.ID)
		if _, busy := out.Commands[key]; busy {
			continue
		}
		var chosen p.Command
		value := 0.0
		for _, pair := range pairs {
			if pair.Role.ID == u.ID && weaponReady(r, pair.Weapon) && p.Distance(pair.Weapon.Pos, u.Pos) <= 1 {
				for _, s := range candidates(r, pair.Weapon, c) {
					value = max(value, s.Value)
				}
			}
		}
		if plan.Emergency && u.Health < 100 && u.Count("Medicine") > 0 {
			chosen = p.Command{Action: "use", Name: "Medicine"}
			value = 1e6
		}
		for _, target := range r.Our.Roles {
			if !critical[target.ID] || repaired[target.ID] || !near(u.Pos, target.Cells()) {
				continue
			}
			name := ""
			switch {
			case target.Type == "wall" && u.Count("WallFixer") > 0:
				name = "WallFixer"
			case target.Type == "station":
				name = "StationUpgradeVoucher" + strconv.Itoa(target.Level)
			case target.Weapon():
				name = "WeaponUpgradeVoucher" + strconv.Itoa(target.Level)
			}
			if name != "" && u.Count(name) > 0 && value < 2e6 && (name == "WallFixer" || target.Level < 3) {
				chosen = p.At("use", target.Pos)
				chosen.Name = name
				value = 2e6
			}
		}
		if value == 0 || plan.Emergency || r.Day() == 10 {
			for _, bot := range r.Robots.Roles {
				if bot.Health <= 0 || !ownThreat(r, bot.Target) {
					continue
				}
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						q := p.Pos{X: bot.Pos.X + dx, Y: bot.Pos.Y + dy}
						if !r.In(q) {
							continue
						}
						d := append([]int(nil), tr.ItemDamage...)
						damage := 0
						delay := 0
						for i, b := range r.Robots.Roles {
							if b.Health > 0 && ownThreat(r, b.Target) && p.Distance(q, b.Pos) <= 1 {
								damage += min(100, max(0, b.Health-d[i]))
								d[i] += 100
								if b.State != "dizzy" && !stunned[b.ID] {
									delay += robotPower(b.Type) * min(c.Strategy.RiskHorizon, 130-(r.Round-1)%130)
								}
							}
						}
						benefit := damageValue(r, d) - damageValue(r, tr.ItemDamage)
						if u.Count("Bomb") > 0 && damage >= c.Strategy.BombMinDamage && benefit > value {
							chosen = p.At("use", q)
							chosen.Name = "Bomb"
							value = benefit
						}
						if (plan.Emergency || value == 0) && delay > 0 && u.Count("DizzyWeapon") > 0 && float64(delay)*10 > value {
							chosen = p.At("use", q)
							chosen.Name = "DizzyWeapon"
							value = float64(delay) * 10
						}
					}
				}
			}
		}
		if chosen.Action == "" {
			continue
		}
		out.Commands[key] = chosen
		for _, target := range r.Our.Roles {
			if len(chosen.Targets) == 1 && chosen.Targets[0] == target.Pos && (chosen.Name == "WallFixer" || chosen.Name == "StationUpgradeVoucher"+strconv.Itoa(target.Level) || chosen.Name == "WeaponUpgradeVoucher"+strconv.Itoa(target.Level)) {
				repaired[target.ID] = true
			}
		}
		if chosen.Name == "Bomb" || chosen.Name == "DizzyWeapon" {
			for i, b := range r.Robots.Roles {
				if p.Distance(b.Pos, chosen.Targets[0]) <= 1 {
					if chosen.Name == "Bomb" {
						tr.ItemDamage[i] += 100
					} else {
						stunned[b.ID] = true
					}
				}
			}
		}
	}
}
