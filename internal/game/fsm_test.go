package game

import (
	p "competition/internal/protocol"
	"testing"
)

func TestFSMPhaseAndTerminalTransitions(t *testing.T) {
	r := small()
	c := DefaultConfig()
	c.EnableNews = false
	r.Our.Roles = r.Our.Roles[:1]
	var m Memory
	for _, tc := range []struct {
		round int
		state string
	}{{1, StateDayEconomy}, {70, StateDayEconomy}, {71, StateNightAssess}, {130, StateNightAssess}, {131, StateDayEconomy}, {1300, StateEnd}} {
		r.Round = tc.round
		out, next, tr := (Engine{c}).Decide(t.Context(), r, m)
		if tr.Strategy.State != tc.state || next.StrategyState != tc.state {
			t.Fatal(tc, tr.Strategy)
		}
		if len(tr.Strategy.Transitions) == 0 {
			t.Fatal("missing transition evidence")
		}
		if !r.Daylight() && len(out.Commands) > 0 {
			t.Fatal("night economy escaped governor", out)
		}
		m = next
	}
	r.Round = 1
	_, m, tr := (Engine{c}).Decide(t.Context(), r, m)
	if tr.Strategy.Transitions[0].To != StateInit || m.StrategyState == StateEnd {
		t.Fatal("new half did not reset")
	}
}

func TestFSMTravelIntentAndCompletion(t *testing.T) {
	r := small()
	c := DefaultConfig()
	c.EnableNews = false
	r.Our.Roles = r.Our.Roles[:1]
	r.Our.Roles[0].Type = "pioneer"
	r.Our.Tasks = []p.PlayerTask{{Pos: p.Pos{X: 5, Y: 5}, Valid: true, Score: 100}}
	out, m, tr := (Engine{c}).Decide(t.Context(), r, Memory{})
	if tr.Strategy.State != StateDayTask || out.Commands["1"].Action != "move" {
		t.Fatal(out, tr.Strategy)
	}
	r.Round++
	r.Our.Tasks = nil
	m.Treasure = &Treasure{Target: p.Pos{X: 5, Y: 5}, Earliest: 1, Latest: 60, Confidence: 1, Items: []string{"sample"}}
	r.Our.Roles[0].Backpack = []string{"sample"}
	out, m, tr = (Engine{c}).Decide(t.Context(), r, m)
	if tr.Strategy.State != StateDayTreasure || out.Commands["1"].Action != "move" {
		t.Fatal(out, tr.Strategy)
	}
	r.Round++
	m.TreasureDone = true
	_, _, tr = (Engine{c}).Decide(t.Context(), r, m)
	if tr.Strategy.State != StateDayEconomy || tr.Strategy.Transitions[0].To != StateDayAssess {
		t.Fatal(tr.Strategy)
	}
}

func TestFSMEmergencyPreemptsAndRecovers(t *testing.T) {
	r := small()
	c := DefaultConfig()
	c.EnableNews = false
	r.Round = 80
	r.Our.Roles = append(r.Our.Roles, p.Role{ID: 90, Type: "station", Level: 1, Health: 1, Pos: p.Pos{X: 5, Y: 5}})
	r.Robots.Roles = []p.Robot{{ID: 100, Type: "bossRobot", Health: 800, Pos: p.Pos{X: 4, Y: 5}}}
	_, m, tr := (Engine{c}).Decide(t.Context(), r, Memory{})
	if tr.Strategy.State != StateNightEmergency || tr.Strategy.Policy.Summon || tr.Strategy.Policy.Work {
		t.Fatal(tr.Strategy)
	}
	r.Round++
	r.Our.Roles[len(r.Our.Roles)-1].Health = 1500
	_, m, tr = (Engine{c}).Decide(t.Context(), r, m)
	if tr.Strategy.State != StateNightDefend || tr.Strategy.Transitions[0].From != StateNightEmergency {
		t.Fatal(tr.Strategy)
	}
	r.Round++
	r.Robots.Roles = nil
	_, _, tr = (Engine{c}).Decide(t.Context(), r, m)
	if tr.Strategy.State != StateNightAssess {
		t.Fatal(tr.Strategy)
	}
}

func TestFSMReturnBlocksNewTaskAndSummon(t *testing.T) {
	r := small()
	c := DefaultConfig()
	r.Round = 70
	r.Our.Roles = r.Our.Roles[:1]
	r.Our.Roles[0].Type = "pioneer"
	r.Our.Roles = append(r.Our.Roles, p.Role{ID: 9, Type: "rocket", Level: 1, Health: 1000, Pos: p.Pos{X: 6, Y: 6}})
	r.Our.Tasks = []p.PlayerTask{{Pos: p.Pos{X: 2, Y: 1}, Valid: true, Score: 10000}}
	out, _, tr := (Engine{c}).Decide(t.Context(), r, Memory{})
	if tr.Strategy.State != StateDayReturn || tr.Strategy.Policy.Task || tr.Strategy.Policy.Summon || out.Commands["1"].Action == "acceptTask" {
		t.Fatal(out, tr.Strategy)
	}
}

func TestFSMBuildIncludesUpgradeAndRepair(t *testing.T) {
	for _, name := range []string{"StationUpgradeVoucher1", "WallFixer"} {
		r := small()
		c := DefaultConfig()
		r.Round = 261
		r.Our.Roles = r.Our.Roles[:1]
		kind := "station"
		if name == "WallFixer" {
			kind = "wall"
		}
		r.Our.Roles = append(r.Our.Roles, p.Role{ID: 9, Type: kind, Level: 1, Health: 100, Pos: p.Pos{X: 5, Y: 5}})
		r.Our.Roles[0].Backpack = []string{name}
		_, _, tr := (Engine{c}).Decide(t.Context(), r, Memory{})
		if tr.Strategy.State != StateDayBuild {
			t.Fatal(name, tr.Strategy)
		}
	}
}

func TestFSMTerminalRequiresEvidenceAndStopsActions(t *testing.T) {
	r := small()
	c := DefaultConfig()
	// An absent enemy base is not proof of destruction.
	if terminalBeforeTurn(r, Memory{}) {
		t.Fatal("fog treated as destruction")
	}
	r.Our.Roles = append(r.Our.Roles, p.Role{Type: "station", Health: 0})
	r.Enemy.Roles = []p.Role{{Type: "station", Health: 0}}
	out, m, tr := (Engine{c}).Decide(t.Context(), r, Memory{})
	if tr.Strategy.State != StateEnd || m.StrategyState != StateEnd || len(out.Commands) > 0 || out.Prompt != "" || out.Execute != "" {
		t.Fatal(out, tr.Strategy)
	}
}

func TestNightPoliciesNeverPermitSummons(t *testing.T) {
	for _, state := range []string{StateNightAssess, StateNightDefend, StateNightEmergency, StateNightOffense, StateEnd} {
		if dispatchPolicy(BattlePlan{State: state}).Summon {
			t.Fatal(state)
		}
	}
	r := small()
	r.Round = 80
	c := DefaultConfig()
	c.Strategy.EnableOffense = true
	r.Our.Roles[0].Backpack = []string{"LargeRobotSummonOrder"}
	proposed := p.Empty()
	proposed.Commands["1"] = p.Command{Action: "use", Name: "LargeRobotSummonOrder"}
	tr := Trace{Rejected: map[string]string{}}
	out := Validate(r, c, proposed, &tr)
	if len(out.Commands) != 0 {
		t.Fatal("night summon escaped final validator")
	}
}

func TestFSMSafetyInterruptsAndRecovery(t *testing.T) {
	for _, cause := range []string{"stuck", "errors", "lost"} {
		r := small()
		r.Round = 2
		c := DefaultConfig()
		m := Memory{Round: 1, StrategyState: StateDayTask}
		switch cause {
		case "stuck":
			m.Stuck = map[int]int{r.Mobiles()[0].ID: 3}
		case "errors":
			m.ActionErrorStreak = 3
		case "lost":
			m.LastPositions = map[int]p.Pos{999: {X: 1, Y: 1}}
		}
		b := assessFSM(t.Context(), r, staticGrid(r), c, m, nil)
		if b.State != StateDayReturn || b.Policy.Task || b.Policy.Work || b.Policy.Summon {
			t.Fatal(cause, b)
		}
		m = Memory{Round: 2, StrategyState: StateDayReturn}
		r.Round = 3
		b = assessFSM(t.Context(), r, staticGrid(r), c, m, nil)
		if b.State != StateDayEconomy {
			t.Fatal("safety state did not recover", b)
		}
	}
}

func TestFSMFinalRoundStillFires(t *testing.T) {
	r := small()
	r.Round = 1300
	c := DefaultConfig()
	c.EnableNews = false
	r.Our.Roles = []p.Role{{ID: 1, Type: "worker", Health: 220, Pos: p.Pos{X: 1, Y: 1}}, {ID: 9, Type: "gatling", Level: 1, Health: 1000, Pos: p.Pos{X: 2, Y: 1}}}
	r.Robots.Roles = []p.Robot{{ID: 99, Type: "smallRobot", Health: 10, Pos: p.Pos{X: 3, Y: 1}}}
	out, m, tr := (Engine{c}).Decide(t.Context(), r, Memory{})
	if out.Commands["9"].Action != "attack" || m.StrategyState != StateEnd {
		t.Fatal(out, tr.Strategy)
	}
	last := tr.Strategy.Transitions[len(tr.Strategy.Transitions)-1]
	if last.From != StateNightDefend || last.To != StateEnd {
		t.Fatal(last)
	}
}
