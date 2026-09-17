package game

import p "competition/internal/protocol"

func weaponReady(r p.Request, w p.Role) bool {
	return !r.Daylight() && (w.Cooldown == nil || *w.Cooldown == 0)
}

func minimumSale(gold, required, price, inventory int) int {
	if price <= 0 || required <= gold {
		return 0
	}
	return min(inventory, (required-gold+price-1)/price)
}

// Milestones and observed damage establish an investment need. This is not a
// claim that invisible future waves or exact survival probabilities are known.
func requiredInvestment(r p.Request, c Config) int {
	required := constructionReserve(r, c)
	day := planDay(r, c)
	for _, u := range r.Our.Roles {
		if u.Health <= 0 || u.Level < 1 || u.Level >= 3 {
			continue
		}
		prefix := ""
		if u.Type == "station" && u.Level < day.BaseLevel {
			prefix = "StationUpgradeVoucher"
		}
		if u.Weapon() && u.Level < day.CoreWeaponLevel {
			prefix = "WeaponUpgradeVoucher"
		}
		if prefix == "" {
			continue
		}
		name := prefix + string(rune('0'+u.Level))
		owned := false
		for _, a := range r.Mobiles() {
			owned = owned || a.Count(name) > 0
		}
		if owned {
			continue
		}
		for _, item := range r.Shop {
			if item.Name == name && item.Price > 0 && (required == 0 || item.Price < required) {
				required = item.Price
			}
		}
	}
	return required
}

// Compare immediate incremental effective output on the current snapshot.
// We do not extrapolate an unverified four-round rocket firing cycle.
func upgradeOutput(r p.Request, u p.Role, c Config) float64 {
	if !u.Weapon() || u.Level >= 3 || u.Cooldown != nil && *u.Cooldown > 0 {
		return 0
	}
	effective := func(w p.Role) int {
		best := 0
		for _, shot := range candidates(r, w, c) {
			total := 0
			for i, d := range shot.Damage {
				if ownThreat(r, r.Robots.Roles[i].Target) {
					total += min(d, r.Robots.Roles[i].Health)
				}
			}
			best = max(best, total)
		}
		return best
	}
	old := effective(u)
	u.Level++
	u.AttackRange = nil
	return float64(max(0, effective(u)-old))
}

type SummonValue struct {
	Evidence                                             bool
	OwnScore, EnemyScore, PressureValue, OpportunityCost float64
}

func worthwhileSummon(v SummonValue) bool {
	return v.Evidence && v.OwnScore+v.PressureValue-v.EnemyScore-v.OpportunityCost > 0
}

func fullTaskScore(reward, timeout int, elapsed float64) float64 {
	if elapsed <= 0 {
		return 0
	}
	return float64(reward) + 5*float64(timeout)/elapsed
}

func assignmentRank(distances []int) (makespan, total int) {
	for _, d := range distances {
		makespan = max(makespan, d)
		total += d
	}
	return
}

// An adapter must refresh this from evidence for each decision, never reuse it across rounds.
type SummonAssessment struct {
	Round int         `json:"round"`
	Value SummonValue `json:"value"`
}
