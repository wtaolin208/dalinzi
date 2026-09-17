package game

import (
	p "competition/internal/protocol"
	"testing"
)

func TestFixedStationsAndDeathReassignment(t *testing.T) {
	r := small()
	r.Our.Roles = []p.Role{{ID: 1, Type: "worker", Health: 220, Pos: p.Pos{X: 1, Y: 1}}, {ID: 2, Type: "worker", Health: 220, Pos: p.Pos{X: 2, Y: 1}}, {ID: 3, Type: "pioneer", Health: 200, Pos: p.Pos{X: 3, Y: 1}}, {ID: 11, Type: "rocket", Health: 1000, Level: 1, Pos: p.Pos{X: 5, Y: 3}}, {ID: 12, Type: "gatling", Health: 1000, Level: 1, Pos: p.Pos{X: 4, Y: 4}}, {ID: 13, Type: "railgun", Health: 1000, Level: 1, Pos: p.Pos{X: 2, Y: 4}}}
	c := DefaultConfig()
	c.Strategy.FixedStations = true // Explicit compatibility mode.
	pairs, _ := defense(t.Context(), r, staticGrid(r), c)
	want := map[int]string{1: "gatling", 2: "railgun", 3: "rocket"}
	if len(pairs) != 3 {
		t.Fatal(pairs)
	}
	for _, pair := range pairs {
		if pair.Weapon.Type != want[pair.Role.ID] {
			t.Fatal("unstable station assignment", pairs)
		}
	}
	r.Our.Roles[2].Health = 0
	pairs, _ = defense(t.Context(), r, staticGrid(r), c)
	if len(pairs) != 2 {
		t.Fatal("death did not trigger reassignment", pairs)
	}
	for _, pair := range pairs {
		if pair.Role.ID == 3 {
			t.Fatal("dead controller")
		}
	}
}

func TestConstructionRespectsEightWayCorridor(t *testing.T) {
	r := small()
	r.Our.Roles = append(r.Our.Roles, p.Role{ID: 9, Type: "station", Health: 1500, Pos: p.Pos{X: 6, Y: 6}})
	g := staticGrid(r)
	m := Memory{RobotHeat: map[string]int{"1,5": 10}}
	value, safe := wallPlanValue(r, g, m, p.Pos{X: 4, Y: 2})
	if !safe || value <= 0 {
		t.Fatal("safe wall rejected")
	}
	// A wall on an observed route is scored below a quiet alternative.
	m.RobotHeat["4,2"] = 20
	hot, _ := wallPlanValue(r, g, m, p.Pos{X: 4, Y: 2})
	if hot >= value {
		t.Fatal("traffic not reflected in corridor score")
	}

}

func TestTreasureRejectsRepeatedWrongItems(t *testing.T) {
	target := Treasure{Target: p.Pos{X: 2, Y: 2}, Earliest: 1, Latest: 60, Items: []string{"a", "b"}, Confidence: .99}
	m := Memory{TreasureHistory: []TreasureAttempt{{Round: 5, Code: 3, Plan: target}}}
	target.Items = []string{"b", "a"}
	if !treasureRejected(m, target) {
		t.Fatal("reordered wrong items retried")
	}
	target.Items = []string{"c"}
	if treasureRejected(m, target) {
		t.Fatal("revised items should be eligible for new evidence")
	}
}

func TestOffenseQuotaAndSafetyGate(t *testing.T) {
	r := small()
	r.Round = 521 // Day 5 permits surplus pressure.
	r.Our.Gold = 500
	c := DefaultConfig()
	c.Strategy.EnableOffense = true
	r.Our.Roles = []p.Role{{ID: 1, Type: "worker", Health: 220, Pos: p.Pos{X: 1, Y: 1}, Backpack: []string{"LargeRobotSummonOrder", "Bomb"}}, {ID: 2, Type: "worker", Health: 220, Pos: p.Pos{X: 2, Y: 1}}, {ID: 3, Type: "pioneer", Health: 200, Pos: p.Pos{X: 3, Y: 1}}, {ID: 10, Type: "station", Health: 3000, Level: 2, Pos: p.Pos{X: 6, Y: 6}}, {ID: 11, Type: "rocket", Health: 1500, Level: 2, Pos: p.Pos{X: 5, Y: 3}}, {ID: 12, Type: "gatling", Health: 1500, Level: 2, Pos: p.Pos{X: 4, Y: 4}}, {ID: 13, Type: "railgun", Health: 1500, Level: 2, Pos: p.Pos{X: 2, Y: 4}}}
	for _, used := range []int{9, 10} {
		m := Memory{SummonsUsed: used}
		out := p.Empty()
		gold := 500
		offenseTurn(r, staticGrid(r), c, &m, BattlePlan{}, &out, map[int][]int{}, &gold)
		if (out.Commands["1"].Name == "LargeRobotSummonOrder") != (used < 10) {
			t.Fatal("daily cap", used, out)
		}
	}
	r.Our.Roles[3].Health = 1
	out := p.Empty()
	gold := 500
	offenseTurn(r, staticGrid(r), c, &Memory{}, BattlePlan{}, &out, map[int][]int{}, &gold)
	if len(out.Commands) > 0 {
		t.Fatal("offense while base is unsafe")
	}
}

func TestEnemyTargetDoesNotTriggerDefenseOrFire(t *testing.T) {
	r := small()
	r.Round = 71
	r.Our.Roles = append(r.Our.Roles, p.Role{ID: 9, Type: "station", Level: 1, Health: 100, Pos: p.Pos{X: 3, Y: 3}})
	r.Robots.Roles = []p.Robot{{ID: 30, Type: "bossRobot", Health: 800, Pos: p.Pos{X: 4, Y: 3}, Target: "defender"}}
	r.Our.Type = "challenger"
	if baseEmergency(r) || damageValue(r, []int{800}) != 0 {
		t.Fatal("helping opponent or retreating from its robot")
	}
	r.Robots.Roles[0].Target = r.Our.Type
	if !baseEmergency(r) {
		t.Fatal("own base threat missed")
	}
	r.Robots.Roles[0].State = "dizzy"
	if baseEmergency(r) {
		t.Fatal("stunned robot treated as immediate attacker")
	}
}

func TestEmergencyUpgradePreemptsController(t *testing.T) {
	r := small()
	r.Round = 71
	r.Our.Roles = r.Our.Roles[:1]
	r.Our.Roles[0].Backpack = []string{"StationUpgradeVoucher1"}
	r.Our.Roles = append(r.Our.Roles, p.Role{ID: 8, Type: "station", Level: 1, Health: 20, Pos: p.Pos{X: 2, Y: 2}}, p.Role{ID: 9, Type: "gatling", Level: 1, Health: 1000, Pos: p.Pos{X: 1, Y: 2}})
	r.Robots.Roles = []p.Robot{{ID: 30, Type: "bossRobot", Health: 800, Pos: p.Pos{X: 4, Y: 2}, Target: r.Our.Type}}
	out, _, tr := (Engine{DefaultConfig()}).Decide(t.Context(), r, Memory{})
	if out.Commands["1"].Name != "StationUpgradeVoucher1" || tr.Strategy.State != "NIGHT_EMERGENCY" || len(tr.Rejected) != 0 {
		t.Fatal(out, tr)
	}
	if _, ok := out.Commands["9"]; ok {
		t.Fatal("upgrading role also controls tower")
	}
}

func TestParallelSubmissionRequiresBothChecksForSkill(t *testing.T) {
	for _, valid := range []bool{false, true} {
		r := learningRequest()
		e := Engine{DefaultConfig()}
		out, m, _ := e.Decide(t.Context(), r, Memory{})
		if out.Prompt == "" || out.Execute == "" {
			t.Fatal("missing initial parallel exploration")
		}
		r.Round++
		answer := "12"
		r.LLM = modelReply(t, ToolPlan{Session: m.Task.Session, Answer: &answer, Skill: sampleSkill()})
		out, m, _ = e.Decide(t.Context(), r, m)
		if out.Execute == "" || out.Commands["1"].Answer == nil || m.SkillReceipt == nil || len(m.Skills) > 0 {
			t.Fatal("must submit now, defer promotion", out, m)
		}
		r.Round++
		r.PhaseTask = ""
		r.Results = map[string]bool{"1": true}
		r.LLM = ""
		r.CmdResult = "[exitCode:0]\n{\"answer\":\"wrong\"}"
		if valid {
			r.CmdResult = "[exitCode:0]\n{\"answer\":\"12\"}"
		}
		_, m, _ = e.Decide(t.Context(), r, m)
		if (len(m.Skills) == 1) != valid {
			t.Fatal("skill admitted without sandbox+judge agreement", valid, m.Skills)
		}
	}
}

func TestMarketEvidenceReserveAndLastDay(t *testing.T) {
	r := small()
	r.News.Official = "明日铜矿停产，铜价上涨"
	m := Memory{News: []NewsEntry{{Day: 1, News: r.News}}}
	c := DefaultConfig()
	acceptForecasts(r, &m, []MarketForecast{{Resource: "copper", StartDay: 2, EndDay: 3, ExpectedTrend: 1, Confidence: .99, EvidenceDay: 1, Evidence: "不存在的新闻"}})
	if len(m.Forecasts) != 0 {
		t.Fatal("accepted fabricated citation")
	}
	acceptForecasts(r, &m, []MarketForecast{{Resource: "copper", StartDay: 2, EndDay: 3, ExpectedTrend: 1, Confidence: .99, EvidenceDay: 1, Evidence: r.News.Official}})
	if marketTrend(r, c, m, "copper") != 1 {
		t.Fatal(m.Forecasts)
	}
	r.Round = 1171
	if marketTrend(r, c, m, "copper") != 0 {
		t.Fatal("hoarding on last day")
	}
	r.Our.Roles = r.Our.Roles[:1]
	r.Our.Roles[0].Backpack = []string{"copper"}
	r.Map.Zones = []p.Zone{{Pos: p.Pos{X: 2, Y: 1}, Type: "vendor"}}
	r.Vendor = []p.ShopItem{{Name: "copper", Price: 5}}
	out, _, _ := (Engine{c}).Decide(t.Context(), r, m)
	if out.Commands["1"].Action != "sell" {
		t.Fatal("final-day liquidation", out)
	}
	c.Profiles[r.Our.Type] = Profile{Verified: true, Walls: []p.Pos{{X: 3, Y: 3}}}
	r.Our.Roles[0].Backpack = nil
	for i := 0; i < 20; i++ {
		r.Our.Roles[0].Backpack = append(r.Our.Roles[0].Backpack, "stone")
	}
	if stoneReserve(r, c, r.Our.Roles[0]) != 8 {
		t.Fatal("wrong team stone reserve")
	}
}

func TestReturnThresholdPerRole(t *testing.T) {
	r := small()
	r.Round = 64
	g := staticGrid(r)
	roles := r.Mobiles()
	pairs := []Pair{{Role: roles[0], Weapon: p.Role{Type: "gatling", Pos: p.Pos{X: 7, Y: 7}}}, {Role: roles[1], Weapon: p.Role{Type: "railgun", Pos: roles[1].Pos}}}
	b := strategyPlan(r, g, DefaultConfig(), pairs)
	if _, ok := b.ReturnRoles[roles[0].ID]; !ok {
		t.Fatal("distant role did not return")
	}
	if _, ok := b.ReturnRoles[roles[1].ID]; ok {
		t.Fatal("nearby role returned too early")
	}
}
