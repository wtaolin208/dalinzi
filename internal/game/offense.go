package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
	"strconv"
)

func constructionReserve(r p.Request, c Config) int {
	return max(0, min(3, len(profile(r, c).Weapons))-len(r.Weapons())) * 25
}
func isSummon(name string) bool {
	return name == "SmallRobotSummonOrder" || name == "MiddleRobotSummonOrder" || name == "LargeRobotSummonOrder" || name == "BossRobotSummonOrder"
}

// Opt-in pressure policy. These gates are conservative heuristics, not a survival proof.
func offenseTurn(r p.Request, g nav.Grid, c Config, m *Memory, plan BattlePlan, out *p.Response, goals map[int][]int, gold *int) {
	day := planDay(r, c)
	// No observed evidence currently establishes positive final-night pressure.
	// Keep cash rather than treating a thick robot as a guaranteed benefit.
	if r.Day() == 10 && (m.SummonValue == nil || m.SummonValue.Round != r.Round || !worthwhileSummon(m.SummonValue.Value)) {
		return
	}
	if !day.AllowOffense || (r.Day() == 4 && m.SummonsUsed >= 1) || !c.Strategy.EnableOffense || !r.Daylight() || plan.Emergency || len(plan.ReturnRoles) > 0 || len(r.Weapons()) < 3 || len(r.Mobiles()) < 3 || m.SummonsUsed >= 10 {
		return
	}
	emergencyStock := false
	basePresent := false
	for _, u := range r.Our.Roles {
		if u.Type == "station" && u.Health > 0 {
			basePresent = true
		}
		if u.Type == "station" || u.Weapon() {
			if u.Health <= 0 || u.Level < 2 || float64(u.Health) < .8*float64(maxStructureHP(u)) {
				return
			}
		}
		emergencyStock = emergencyStock || u.Count("Bomb") > 0 || u.Count("DizzyWeapon") > 0
	}
	if !basePresent || !emergencyStock {
		return
	}
	name := "LargeRobotSummonOrder"
	rockets, single := 0, 0
	for _, u := range r.Enemy.Roles {
		if u.Health > 0 {
			if u.Type == "rocket" {
				rockets++
			}
			if u.Type == "gatling" || u.Type == "railgun" {
				single++
			}
		}
	}
	if rockets == 0 && single >= 2 {
		name = "SmallRobotSummonOrder"
	}
	for _, u := range r.Mobiles() {
		key := strconv.Itoa(u.ID)
		if _, ok := out.Commands[key]; ok {
			continue
		}
		if _, ok := goals[u.ID]; ok {
			continue
		}
		if u.Count(name) > 0 {
			out.Commands[key] = p.Command{Action: "use", Name: name}
			return
		}
		if u.Full() {
			continue
		}
		for _, item := range r.Shop {
			if item.Name == name && item.Price > 0 && *gold-item.Price >= c.Strategy.OffenseGoldReserve {
				path := g.Shortest(g.ID(u.Pos), g.Around(zones(r, "weaponShop")))
				if len(path) == 0 {
					continue
				}
				if len(path)+2+roleReturnDistance(r, g, c, u, g.Pos(path[len(path)-1]))+c.ReturnBuffer >= r.DayLeft() {
					continue
				}
				// Prefer defense over giving the opponent a cheap source of kill points.
				enemyKillPoints := 4
				if name == "SmallRobotSummonOrder" {
					enemyKillPoints = 1
				}
				pressureHP := 500
				if name == "SmallRobotSummonOrder" {
					pressureHP = 40
				}
				if pressureHP <= enemyKillPoints*20 {
					continue
				}
				if moveOr(r, u, g, zones(r, "weaponShop"), p.Command{Action: "buy", Name: name, Num: 1}, out, goals) {
					if near(u.Pos, zones(r, "weaponShop")) {
						*gold -= item.Price
					}
					return
				}
			}
		}
	}
}
