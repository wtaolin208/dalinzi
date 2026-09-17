package game

import (
	p "competition/internal/protocol"
	"encoding/json"
	"os"
	"testing"
)

func scenario(t *testing.T, id string) map[string]any {
	t.Helper()
	b, err := os.ReadFile("testdata/strategy_scenarios.json")
	if err != nil {
		t.Fatal(err)
	}
	var all []map[string]any
	if err = json.Unmarshal(b, &all); err != nil {
		t.Fatal(err)
	}
	for _, f := range all {
		if f["id"] == id {
			t.Logf("scenario=%s requirements=%v expected=%v", id, f["requires"], f["expected"])
			return f
		}
	}
	t.Fatal("unknown fixture", id)
	return nil
}
func fixtureInt(m map[string]any, key string) int { return int(m[key].(float64)) }

func TestStrategyS01(t *testing.T) {
	f := scenario(t, "S01")
	in := f["input"].(map[string]any)
	for i, elapsed := range in["full_completion_elapsed"].([]any) {
		got := fullTaskScore(fixtureInt(in, "score_reward"), fixtureInt(in, "timeout_rounds"), elapsed.(float64))
		if got != []float64{70, 50, 35}[i] {
			t.Fatal(got)
		}
	}
	r := small()
	r.Our.Roles = r.Our.Roles[:1]
	r.Our.Roles[0].Type = "pioneer"
	timeout := fixtureInt(in, "timeout_rounds")
	r.Our.Tasks = []p.PlayerTask{{Valid: true, Score: fixtureInt(in, "score_reward"), Timeout: &timeout, Pos: p.Pos{X: 2, Y: 1}}}
	out, _, _ := (Engine{DefaultConfig()}).Decide(t.Context(), r, Memory{})
	if out.Commands["1"].Action != "acceptTask" {
		t.Fatal(out)
	}
	// Negative: defense now requires the pioneer before night.
	r.Round = 70
	r.Our.Roles = append(r.Our.Roles, p.Role{ID: 9, Type: "rocket", Health: 1000, Level: 1, Pos: p.Pos{X: 6, Y: 6}})
	out, _, _ = (Engine{DefaultConfig()}).Decide(t.Context(), r, Memory{})
	if out.Commands["1"].Action == "acceptTask" {
		t.Fatal("task displaced defense")
	}
}
func TestStrategyS02(t *testing.T) {
	f := scenario(t, "S02")
	in := f["input"].(map[string]any)
	rounds := fixtureInt(in, "travel_to_B") + fixtureInt(in, "accept_actions") + fixtureInt(in, "solve_elapsed_after_accept") + fixtureInt(in, "travel_back")
	if rounds != 14 || fixtureInt(in, "A_cooldown")-rounds != 16 {
		t.Fatal(rounds)
	}
	r := small()
	r.Our.Roles = r.Our.Roles[:1]
	r.Our.Roles[0].Type = "pioneer"
	a := p.PlayerTask{Pos: p.Pos{X: 2, Y: 1}, Valid: true, Cooldown: 30, Score: 9999}
	b := p.PlayerTask{Pos: p.Pos{X: 5, Y: 5}, Valid: true, Score: 20}
	r.Our.Tasks = []p.PlayerTask{a, b}
	c := DefaultConfig()
	chosen := chooseTask(r, r.Our.Roles[0], staticGrid(r), c, Memory{})
	if chosen == nil || chosen.Pos != b.Pos {
		t.Fatal(chosen)
	}
	// Negative: active task A remains occupied; do not leave for B.
	r.PhaseTask = "solve the active task"
	out, _, _ := (Engine{c}).Decide(t.Context(), r, Memory{Team: r.Our.ID, Side: r.Our.Type})
	if out.Commands["1"].Action == "move" || out.Commands["1"].Action == "acceptTask" {
		t.Fatal(out)
	}
}
func TestStrategyS03(t *testing.T) {
	scenario(t, "S03")
	if fullTaskScore(20, 30, 3)-fullTaskScore(20, 30, 5) != 20 {
		t.Fatal("delay loss")
	}
	r := learningRequest()
	r.Round = 4
	c := DefaultConfig()
	m := Memory{Team: r.Our.ID, Side: r.Our.Type, Round: 3, Task: TaskMemory{Text: r.PhaseTask, Start: 1, Answer: "candidate", Timeout: 30}}
	out, _, _ := (Engine{c}).Decide(t.Context(), r, m)
	found := false
	for _, cmd := range out.Commands {
		found = found || cmd.Action == "submitAnswer"
	}
	if !found {
		t.Fatal("ready candidate delayed", out)
	}
	m.Task.Answer = ""
	out, _, _ = (Engine{c}).Decide(t.Context(), r, m)
	for _, cmd := range out.Commands {
		if cmd.Action == "submitAnswer" {
			t.Fatal("empty candidate submitted")
		}
	}
}
func combatFixture() (p.Request, Config) {
	r := small()
	r.Map.Width = 41
	r.Map.Height = 32
	r.Round = 80
	r.Our.Roles = nil
	return r, DefaultConfig()
}
func checkedFight(t *testing.T, r p.Request, c Config, pairs []Pair) p.Response {
	t.Helper()
	out := p.Empty()
	tr := Trace{Rejected: map[string]string{}}
	fight(r, pairs, c, Memory{}, &out, &tr)
	valid := Validate(r, c, out, &tr)
	if len(tr.Rejected) > 0 {
		t.Fatal(tr.Rejected)
	}
	t.Logf("selected=%+v", valid.Commands)
	return valid
}
func TestStrategyS04(t *testing.T) {
	scenario(t, "S04")
	r, c := combatFixture()
	c.CombatBeam = 1
	g := p.Role{ID: 10, Type: "gatling", Level: 1, Health: 1000, Pos: p.Pos{X: 2, Y: 2}}
	rail := p.Role{ID: 11, Type: "railgun", Level: 3, Health: 2000, Pos: p.Pos{X: 2, Y: 6}}
	u := p.Role{ID: 1, Type: "worker", Health: 220, Pos: p.Pos{X: 1, Y: 2}}
	v := p.Role{ID: 2, Type: "worker", Health: 220, Pos: p.Pos{X: 1, Y: 6}}
	r.Our.Roles = []p.Role{u, v, g, rail}
	a := p.Pos{X: 4, Y: 2}
	b := p.Pos{X: 10, Y: 6}
	r.Robots.Roles = []p.Robot{{ID: 90, Type: "middleRobot", Health: 10, Pos: a}, {ID: 91, Type: "middleRobot", Health: 30, Pos: b}}
	out := checkedFight(t, r, c, []Pair{{u, g}, {v, rail}})
	if len(out.Commands) != 2 || out.Commands["10"].Targets[0] != a || out.Commands["11"].Targets[0] != b {
		t.Fatal(out)
	}
	r.Robots.Roles[1].Pos = p.Pos{X: 35, Y: 6}
	out = checkedFight(t, r, c, []Pair{{u, g}, {v, rail}})
	for _, cmd := range out.Commands {
		for _, q := range cmd.Targets {
			if q == r.Robots.Roles[1].Pos {
				t.Fatal("out-of-range target")
			}
		}
	}
}
func TestStrategyS05(t *testing.T) {
	f := scenario(t, "S05")
	r, c := combatFixture()
	w := p.Role{ID: 9, Type: "railgun", Level: 3, Health: 2000, Pos: p.Pos{X: 1, Y: 5}}
	u := p.Role{ID: 1, Type: "worker", Health: 220, Pos: p.Pos{X: 1, Y: 4}}
	r.Our.Roles = []p.Role{u, w}
	hp := f["input"].(map[string]any)["ordered_hp"].([]any)
	for i, h := range hp {
		r.Robots.Roles = append(r.Robots.Roles, p.Robot{ID: 90 + i, Type: "middleRobot", Health: int(h.(float64)), Pos: p.Pos{X: 3 + i*2, Y: 5}})
	}
	out := checkedFight(t, r, c, []Pair{{u, w}})
	if out.Commands["9"].Targets[0].X < 7 {
		t.Fatal("endpoint wastes energy", out)
	}
	d := shotDamage(r, w, out.Commands["9"].Targets)
	first := shotDamage(r, w, []p.Pos{r.Robots.Roles[0].Pos})
	if first[0] != 5 || first[1] != 0 || first[2] != 0 {
		t.Fatal("endpoint did not stop energy", first)
	}
	for i, want := range []int{5, 10, 15} {
		if d[i] != want {
			t.Fatal(d)
		}
	}
	r.Robots.Roles[2].Pos.X = 30
	out = checkedFight(t, r, c, []Pair{{u, w}})
	if out.Commands["9"].Targets[0].X == 30 {
		t.Fatal("range bypass")
	}
}
func TestStrategyS06(t *testing.T) {
	scenario(t, "S06")
	r, c := combatFixture()
	ready := 0
	w := p.Role{ID: 9, Type: "rocket", Level: 1, Health: 1000, Pos: p.Pos{X: 5, Y: 10}, Cooldown: &ready}
	u := p.Role{ID: 1, Type: "worker", Health: 220, Pos: p.Pos{X: 5, Y: 9}}
	r.Our.Roles = []p.Role{u, w}
	r.Robots.Roles = []p.Robot{{ID: 90, Type: "middleRobot", Health: 10, Pos: p.Pos{X: 10, Y: 10}}, {ID: 91, Type: "middleRobot", Health: 10, Pos: p.Pos{X: 12, Y: 10}}}
	out := checkedFight(t, r, c, []Pair{{u, w}})
	q := out.Commands["9"].Targets[0]
	for _, bot := range r.Robots.Roles {
		if p.Distance(q, bot.Pos) > 1 {
			t.Fatal("missed splash kill", q)
		}
	}
	ready = 1
	out = checkedFight(t, r, c, []Pair{{u, w}})
	if len(out.Commands) > 0 {
		t.Fatal("cooldown ignored")
	}
}
func TestStrategyS07(t *testing.T) {
	scenario(t, "S07")
	r, c := combatFixture()
	cool := 2
	g := p.Role{ID: 10, Type: "gatling", Level: 1, Health: 1000, Pos: p.Pos{X: 2, Y: 2}}
	rail := p.Role{ID: 11, Type: "railgun", Level: 3, Health: 2000, Pos: p.Pos{X: 2, Y: 6}}
	rocket := p.Role{ID: 12, Type: "rocket", Level: 1, Health: 1000, Cooldown: &cool, Pos: p.Pos{X: 10, Y: 10}}
	u := p.Role{ID: 1, Type: "worker", Health: 220, Pos: p.Pos{X: 1, Y: 2}}
	v := p.Role{ID: 2, Type: "worker", Health: 220, Pos: p.Pos{X: 1, Y: 6}}
	h := p.Role{ID: 3, Type: "pioneer", Health: 200, Pos: p.Pos{X: 10, Y: 9}, Backpack: []string{"DizzyWeapon"}}
	r.Our.Roles = []p.Role{u, v, h, g, rail, rocket}
	r.Robots.Roles = []p.Robot{{ID: 90, Type: "middleRobot", Health: 10, Pos: p.Pos{X: 4, Y: 2}}, {ID: 91, Type: "middleRobot", Health: 30, Pos: p.Pos{X: 10, Y: 6}}, {ID: 92, Type: "bossRobot", Health: 800, Pos: p.Pos{X: 15, Y: 15}}}
	run := func() p.Response {
		pairs := []Pair{{u, g}, {v, rail}, {h, rocket}}
		out := p.Empty()
		tr := Trace{Rejected: map[string]string{}}
		emergencyItems(r, c, BattlePlan{}, pairs, &out, &tr)
		fight(r, pairs, c, Memory{}, &out, &tr)
		out = Validate(r, c, out, &tr)
		if len(tr.Rejected) > 0 {
			t.Fatal(tr.Rejected)
		}
		return out
	}
	out := run()
	if out.Commands["3"].Name != "DizzyWeapon" || out.Commands["10"].Action != "attack" || out.Commands["11"].Action != "attack" || out.Commands["12"].Action != "" {
		t.Fatal(out)
	}
	h.Backpack = nil
	r.Our.Roles[2] = h
	out = run()
	if out.Commands["3"].Action == "use" {
		t.Fatal("invented inventory")
	}
}
func TestStrategyS11(t *testing.T) {
	f := scenario(t, "S11")
	in := f["input"].(map[string]any)
	r := small()
	r.Round = 261
	r.Our.Gold = fixtureInt(in, "gold")
	r.Our.Roles = r.Our.Roles[:1]
	for i := 0; i < fixtureInt(in, "iron_inventory"); i++ {
		r.Our.Roles[0].Backpack = append(r.Our.Roles[0].Backpack, "iron")
	}
	r.Our.Roles = append(r.Our.Roles, p.Role{ID: 9, Type: "station", Level: 1, Health: 1500, Pos: p.Pos{X: 6, Y: 6}})
	r.Vendor = []p.ShopItem{{Name: "iron", Price: fixtureInt(in, "price_now")}}
	r.Shop = []p.ShopItem{{Name: "StationUpgradeVoucher1", Price: fixtureInt(in, "required_purchase_cost")}}
	r.Map.Zones = []p.Zone{{Type: "vendor", Pos: p.Pos{X: 2, Y: 1}}, {Type: "weaponShop", Pos: p.Pos{X: 1, Y: 2}}}
	m := Memory{Team: r.Our.ID, Side: r.Our.Type, Forecasts: []MarketForecast{{Resource: "iron", StartDay: 4, EndDay: 4, Confidence: 1, ExpectedTrend: 1, EvidenceDay: 3}}}
	out, _, _ := (Engine{DefaultConfig()}).Decide(t.Context(), r, m)
	if cmd := out.Commands["1"]; cmd.Action != "sell" || cmd.Num != 2 {
		t.Fatal(out)
	}
	cmd := out.Commands["1"]
	if r.Our.Gold+cmd.Num*5 != 100 || len(r.Our.Roles[0].Backpack)-cmd.Num != 8 || cmd.Num*(10-5) != 10 {
		t.Fatal("sale outcome")
	}
	r.Our.Roles[1].Level = 2
	r.Our.Roles[1].Health = 3000
	out, _, _ = (Engine{DefaultConfig()}).Decide(t.Context(), r, m)
	if out.Commands["1"].Action == "sell" {
		t.Fatal("unneeded liquidation", out)
	}
}
func TestStrategyS12(t *testing.T) {
	scenario(t, "S12")
	for _, tc := range []struct {
		a, b   string
		ha, hb int
		want   int
	}{{"largeRobot", "middleRobot", 500, 10, 1}, {"bossRobot", "middleRobot", 10, 60, 0}} {
		r, c := combatFixture()
		r.Round = 1293
		w := p.Role{ID: 9, Type: "gatling", Health: 1000, Level: 1, Pos: p.Pos{X: 3, Y: 3}}
		u := p.Role{ID: 1, Type: "worker", Health: 220, Pos: p.Pos{X: 2, Y: 3}, Backpack: []string{"StationUpgradeVoucher1"}}
		r.Our.Roles = []p.Role{u, w, {ID: 8, Type: "station", Health: 1000, Level: 1, Pos: p.Pos{X: 1, Y: 3}}}
		r.Robots.Roles = []p.Robot{{ID: 90, Type: tc.a, Health: tc.ha, Pos: p.Pos{X: 5, Y: 3}}, {ID: 91, Type: tc.b, Health: tc.hb, Pos: p.Pos{X: 3, Y: 5}}}
		out, _, _ := (Engine{c}).Decide(t.Context(), r, Memory{})
		cmd := out.Commands["9"]
		if cmd.Action != "attack" || cmd.Targets[0] != r.Robots.Roles[tc.want].Pos {
			t.Fatal(out)
		}
		// Negative: immediate lethal threat restores emergency priority.
		r.Our.Roles[2].Health = 1
		_, _, tr := (Engine{c}).Decide(t.Context(), r, Memory{})
		if tr.Strategy.State != StateNightEmergency {
			t.Fatal(tr.Strategy)
		}
	}
}
func TestStrategyS13(t *testing.T) {
	f := scenario(t, "S13")
	in := f["input"].(map[string]any)
	v := SummonValue{Evidence: true, OwnScore: float64(fixtureInt(in, "our_score_gain")), EnemyScore: float64(fixtureInt(in, "enemy_score_gain"))}
	if worthwhileSummon(v) {
		t.Fatal("accepted guaranteed negative margin")
	}
	v.PressureValue = 1000
	if !worthwhileSummon(v) {
		t.Fatal("verified breakthrough ignored")
	}
	v.Evidence = false
	if worthwhileSummon(v) {
		t.Fatal("unproven breakthrough")
	}
}
func TestStrategyS14(t *testing.T) {
	f := scenario(t, "S14")
	matrix := f["input"].(map[string]any)["distance_matrix"].([]any)
	bestMax, bestTotal := 999, 999
	var best []int
	for a := 0; a < 3; a++ {
		for b := 0; b < 3; b++ {
			if b == a {
				continue
			}
			c := 3 - a - b
			cols := []int{a, b, c}
			ds := []int{}
			for i, col := range cols {
				ds = append(ds, int(matrix[i].([]any)[col].(float64)))
			}
			mx, total := assignmentRank(ds)
			if mx < bestMax || mx == bestMax && total < bestTotal {
				bestMax, bestTotal, best = mx, total, cols
			}
		}
	}
	if bestMax != 2 || bestTotal != 6 || best[0] != 1 || best[1] != 0 || best[2] != 2 {
		t.Fatal(bestMax, bestTotal, best)
	}
	r, c := combatFixture()
	r.Round = 60
	r.Our.Roles = []p.Role{{ID: 1, Type: "worker", Health: 220, Pos: p.Pos{X: 12, Y: 2}}, {ID: 2, Type: "worker", Health: 220, Pos: p.Pos{X: 2, Y: 2}}, {ID: 3, Type: "pioneer", Health: 200, Pos: p.Pos{X: 7, Y: 12}}, {ID: 10, Type: "gatling", Health: 1000, Level: 1, Pos: p.Pos{X: 2, Y: 4}}, {ID: 11, Type: "railgun", Health: 1000, Level: 1, Pos: p.Pos{X: 12, Y: 4}}, {ID: 12, Type: "rocket", Health: 1000, Level: 1, Pos: p.Pos{X: 7, Y: 14}}}
	pairs, traces := defense(t.Context(), r, staticGrid(r), c)
	if len(traces) != 6 {
		t.Fatal("not all assignments evaluated", len(traces))
	}
	for _, pair := range pairs {
		want := map[int]int{1: 11, 2: 10, 3: 12}[pair.Role.ID]
		if pair.Weapon.ID != want {
			t.Fatal(pairs)
		}
	}
	for _, tr := range traces {
		for step := 1; step < len(tr.Result.Path); step++ {
			a, b := tr.Result.Path[step-1], tr.Result.Path[step]
			for i := 0; i < 3; i++ {
				for j := 0; j < i; j++ {
					if b[i] == b[j] || b[i] == a[j] && b[j] == a[i] {
						t.Fatal("vertex or swap conflict")
					}
				}
			}
		}
	}
	// Negative: blocked parking invalidates the original short assignment.
	g := staticGrid(r)
	for _, id := range g.Around([]p.Pos{r.Our.Roles[4].Pos}) {
		g.Block[id] = true
	}
	_, changed := defense(t.Context(), r, g, c)
	for _, tr := range changed {
		if tr.Result.Cost == 1 {
			t.Fatal("kept impossible one-round plan")
		}
	}
}

func TestStrategyS10A(t *testing.T) {
	scenario(t, "S10")
	r, c := combatFixture()
	g := p.Role{ID: 10, Type: "gatling", Level: 1, Health: 1000, Pos: p.Pos{X: 0, Y: 0}}
	w := p.Role{ID: 11, Type: "railgun", Level: 1, Health: 1000, Pos: p.Pos{X: 2, Y: 2}}
	r.Our.Roles = []p.Role{g, w}
	r.Robots.Roles = []p.Robot{{ID: 90, Type: "largeRobot", Health: 500, Pos: p.Pos{X: 7, Y: 7}}}
	if upgradeOutput(r, w, c) != 10 || upgradeOutput(r, g, c) != 0 {
		t.Fatal("incremental effective damage")
	}
	day := DayPlan{Day: 3, CoreWeaponLevel: 2}
	if upgradePriority(r, w, day, c) <= upgradePriority(r, g, day, c) {
		t.Fatal("upgrade ignored marginal output")
	}
	blocked := 1
	w.Cooldown = &blocked
	if upgradeOutput(r, w, c) != 0 {
		t.Fatal("cooldown has no immediate output opportunity")
	}
}
