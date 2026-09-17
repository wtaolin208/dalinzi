package game

import p "competition/internal/protocol"

func observeNightRisk(r p.Request, c Config, m *Memory) {
	if r.Daylight() {
		return
	}
	if m.NightBaseRisk == nil {
		m.NightBaseRisk = map[int]bool{}
	}
	for _, u := range r.Our.Roles {
		if u.Type != "station" {
			continue
		}
		remaining := u.Health
		for _, risk := range assessStructures(r, c) {
			if risk.ID == u.ID {
				remaining -= risk.HorizonDamage
			}
		}
		if float64(remaining) < float64(maxStructureHP(u))*c.Strategy.EmergencyStationHpRatio {
			m.NightBaseRisk[r.Day()] = true
		}
	}
}
func repeatedNightRisk(r p.Request, m Memory) bool {
	return r.Day() >= 7 && m.NightBaseRisk[r.Day()-1] && m.NightBaseRisk[r.Day()-2]
}

// DayPlan describes milestones; candidate modules enforce them against the
// authoritative snapshot. A target level is a priority, never a promised result.
type DayPlan struct {
	Day              int    `json:"day"`
	BaseLevel        int    `json:"baseLevel"`
	CoreWeaponLevel  int    `json:"coreWeaponLevel"`
	WallLevel        int    `json:"wallLevel"`
	FreezeStructures bool   `json:"freezeStructures"`
	AllowOffense     bool   `json:"allowOffense"`
	RiskMode         string `json:"riskMode"`
	Focus            string `json:"focus"`
}

func planDay(r p.Request, c Config) DayPlan {
	d := r.Day()
	s := assessScore(r, c)
	p := DayPlan{Day: d, BaseLevel: 1, CoreWeaponLevel: 1, WallLevel: 1, RiskMode: s.RiskMode, Focus: "three weapons and short tasks"}
	if d >= 2 {
		p.Focus = "repair first-night damage and establish trade routes"
	}
	if d >= 3 {
		p.BaseLevel = 2
		p.CoreWeaponLevel = 2
		p.Focus = "base and core weapon upgrades"
	}
	if d >= 6 {
		p.Focus = "short reusable tasks and stronger defense"
	}
	if d >= 7 {
		p.BaseLevel = 3
		p.WallLevel = 2
	}
	if d >= 8 {
		p.FreezeStructures = true
		p.Focus = "preserve formation; upgrades, repairs and quick tasks"
	}
	if d >= 9 {
		p.Focus = "realize inventory value and buy immediately useful supplies"
	}
	if d == 10 {
		p.Focus = "final-day liquidation and score-aware defense"
	}
	p.AllowOffense = d >= 4 && !(d >= 8 && s.RiskMode == "lead")
	if baseEmergency(r, c) {
		p.AllowOffense = false
		p.Focus = "emergency survival"
	}
	return p
}
func upgradePriority(r p.Request, u p.Role, day DayPlan, c Config) float64 {
	target := 1
	switch {
	case u.Type == "station":
		target = day.BaseLevel
	case u.Weapon():
		target = day.CoreWeaponLevel
	case u.Type == "wall":
		target = day.WallLevel
	}
	for _, risk := range assessStructures(r, c) {
		if risk.ID == u.ID && risk.Critical {
			return 100000
		}
	}
	if u.Level < target {
		if u.Type == "station" {
			return 500
		}
		return 300 + upgradeOutput(r, u, c)
	}
	// Avoid spending the first upgrade budget on surplus levels or buying
	// healing vouchers for only a scratch. Injury is an explicit exception.
	if day.Day >= 2 && u.Health*2 < maxStructureHP(u) {
		return 200
	}
	return 0
}
