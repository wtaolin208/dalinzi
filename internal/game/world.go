package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
	"fmt"
	"math"
)

type StrategyConfig struct {
	TaskMinExpectedDensity  float64  `json:"taskMinExpectedDensity"`
	LateTaskMaxRounds       int      `json:"lateTaskMaxRounds"`
	ScoreMarginBuffer       int      `json:"scoreMarginBuffer"`
	FixedStations           bool     `json:"fixedStations"`
	EnableOffense           bool     `json:"enableOffense"`
	OffenseGoldReserve      int      `json:"offenseGoldReserve"`
	StoneReserveMin         int      `json:"stoneReserveMin"`
	StoneReserveMax         int      `json:"stoneReserveMax"`
	TaskMinFinishBuffer     int      `json:"taskMinFinishBuffer"`
	TreasureConfidence      float64  `json:"treasureConfidence"`
	ForecastConfidence      float64  `json:"forecastConfidence"`
	ForecastPremium         float64  `json:"forecastPremium"`
	EmergencyStationHpRatio float64  `json:"emergencyStationHpRatio"`
	WallRepairHpRatio       float64  `json:"wallRepairHpRatio"`
	RiskHorizon             int      `json:"riskHorizon"`
	BombMinDamage           int      `json:"bombMinDamage"`
	FireOrder               []string `json:"fireOrder"`
	SubmitFirst             bool     `json:"submitFirst"`
	ParallelTask            bool     `json:"parallelTask"`
}

func defaultStrategy() StrategyConfig {
	return StrategyConfig{LateTaskMaxRounds: 12, ScoreMarginBuffer: 20, FixedStations: false, EnableOffense: false, OffenseGoldReserve: 100, StoneReserveMin: 8, StoneReserveMax: 15, TaskMinFinishBuffer: 5, TreasureConfidence: .85, ForecastConfidence: .9, ForecastPremium: .25, EmergencyStationHpRatio: .30, WallRepairHpRatio: .35, RiskHorizon: 3, BombMinDamage: 100, FireOrder: []string{"rocket", "railgun", "gatling"}, SubmitFirst: true, ParallelTask: true}
}
func (c StrategyConfig) Validate() error {
	if c.TaskMinExpectedDensity < 0 || math.IsNaN(c.TaskMinExpectedDensity) || math.IsInf(c.TaskMinExpectedDensity, 0) || c.LateTaskMaxRounds < 1 || c.ScoreMarginBuffer < 0 || c.OffenseGoldReserve < 0 || c.StoneReserveMin < 0 || c.StoneReserveMax < c.StoneReserveMin || c.TaskMinFinishBuffer < 0 || c.RiskHorizon < 1 || c.RiskHorizon > 10 || c.BombMinDamage < 1 {
		return fmt.Errorf("invalid strategy reserve/budget")
	}
	for _, x := range []float64{c.TreasureConfidence, c.ForecastConfidence, c.ForecastPremium, c.EmergencyStationHpRatio, c.WallRepairHpRatio} {
		if math.IsNaN(x) || math.IsInf(x, 0) || x < 0 || x > 1 {
			return fmt.Errorf("invalid strategy ratio")
		}
	}
	seen := map[string]bool{}
	for _, s := range c.FireOrder {
		if seen[s] || s != "rocket" && s != "railgun" && s != "gatling" {
			return fmt.Errorf("invalid fire order")
		}
		seen[s] = true
	}
	if len(seen) != 3 {
		return fmt.Errorf("fire order must include all three weapons")
	}
	return nil
}

// Threats are conservative proximity estimates, not an oracle for robot actions.
type StructureRisk struct {
	ID            int    `json:"id"`
	Kind          string `json:"kind"`
	NextDamage    int    `json:"nextDamage"`
	HorizonDamage int    `json:"horizonDamage"`
	Critical      bool   `json:"critical"`
}
type BattlePlan struct {
	Transitions []StateTransition `json:"transitions"`
	Policy      DispatchPolicy    `json:"policy"`
	State       string            `json:"state"`
	Day         int               `json:"day"`
	DayLeft     int               `json:"dayLeft"`
	Emergency   bool              `json:"emergency"`
	ReturnRoles map[int]int       `json:"returnRoles"`
	Risks       []StructureRisk   `json:"risks"`
	Reason      string            `json:"reason"`
}

func ownThreat(r p.Request, target string) bool { return target == "" || target == r.Our.Type }
func robotPower(kind string) int {
	return map[string]int{"smallRobot": 5, "middleRobot": 10, "largeRobot": 20, "bossRobot": 40}[kind]
}
func maxStructureHP(u p.Role) int {
	l := min(3, max(1, u.Level)) - 1
	switch u.Type {
	case "station":
		return []int{1500, 3000, 4500}[l]
	}
	return []int{1000, 1500, 2000}[l]
}
func assessStructures(r p.Request, c Config) []StructureRisk {
	var risks []StructureRisk
	for _, u := range r.Our.Roles {
		if u.Health <= 0 || u.Mobile() {
			continue
		}
		x := StructureRisk{ID: u.ID, Kind: u.Type}
		for _, bot := range r.Robots.Roles {
			if bot.Health <= 0 || bot.State == "dizzy" || !ownThreat(r, bot.Target) {
				continue
			}
			dist := 10000
			for _, pos := range u.Cells() {
				dist = min(dist, p.Distance(pos, bot.Pos))
			}
			power := robotPower(bot.Type)
			if dist <= 4 {
				x.NextDamage += power
			}
			if dist <= 3+c.Strategy.RiskHorizon {
				x.HorizonDamage += power * c.Strategy.RiskHorizon
			}
		}
		ratio := c.Strategy.EmergencyStationHpRatio
		if u.Type == "wall" {
			ratio = c.Strategy.WallRepairHpRatio
		}
		x.Critical = x.NextDamage >= u.Health || x.HorizonDamage >= u.Health || x.HorizonDamage > 0 && (u.Type == "station" || u.Type == "wall") && float64(u.Health) <= float64(maxStructureHP(u))*ratio
		risks = append(risks, x)
	}
	return risks
}
func strategyPlan(r p.Request, g nav.Grid, c Config, pairs []Pair) BattlePlan {
	b := BattlePlan{State: "DAY_ECONOMY", Day: r.Day(), DayLeft: r.DayLeft(), ReturnRoles: map[int]int{}, Risks: assessStructures(r, c)}
	for _, x := range b.Risks {
		if x.Critical {
			b.Emergency = true
		}
	}
	for _, pair := range pairs {
		path := g.Shortest(g.ID(pair.Role.Pos), g.Around(pair.Weapon.Cells()))
		distance := 10000
		if len(path) > 0 {
			distance = len(path) - 1
		}
		if !r.Daylight() || r.DayLeft() <= distance+c.ReturnBuffer || b.Emergency {
			b.ReturnRoles[pair.Role.ID] = distance
		}
	}
	switch {
	case b.Emergency:
		b.State = "NIGHT_EMERGENCY"
		if r.Daylight() {
			b.State = StateDayBuild
		}
		b.Reason = "predicted structure damage"
	case !r.Daylight():
		b.State = "NIGHT_DEFEND"
	case len(b.ReturnRoles) > 0:
		b.State = "DAY_RETURN_HOME"
	case len(r.Weapons()) < 3 && len(profile(r, c).Weapons) > 0:
		b.State = "DAY_BUILD"
	case r.PhaseTask != "":
		b.State = "DAY_TASK"
	}
	return b
}
