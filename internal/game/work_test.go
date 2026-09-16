package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
	"context"
	"strings"
	"testing"
)

func workFixture() (p.Request, Config) {
	r := small()
	r.Our.Gold = 50
	r.Our.Roles[0].Pos = p.Pos{X: 1, Y: 1}
	r.Our.Roles[1].Pos = p.Pos{X: 6, Y: 6}
	c := DefaultConfig()
	// Deliberately reversed: the old first-worker-first-site policy crosses routes.
	c.Profiles[r.Our.Type] = Profile{Verified: true, Weapons: []Site{{p.Pos{X: 5, Y: 6}, "gatling"}, {p.Pos{X: 1, Y: 2}, "railgun"}}}
	return r, c
}

func TestWorkersJointlyChooseNearestBuildAssignment(t *testing.T) {
	r, c := workFixture()
	out, m, tr := (Engine{Config: c}).Decide(t.Context(), r, Memory{})
	if out.Commands["1"].Action != "build" || out.Commands["1"].Targets[0] != (p.Pos{X: 1, Y: 2}) || out.Commands["2"].Action != "build" || out.Commands["2"].Targets[0] != (p.Pos{X: 5, Y: 6}) {
		t.Fatal("workers did not exchange targets", out, tr.Team)
	}
	if tr.Team == nil || !tr.Team.Proven || len(m.Work) != 2 {
		t.Fatal(tr.Team, m.Work)
	}
	// Exhaust all two-site permutations with independent single-role distances:
	// zero travel is a lower bound and both workers achieve it simultaneously.
	g := staticGrid(r)
	// Keep the old sequential policy as an explicit comparison baseline.
	oldOut := p.Empty()
	oldGoals := map[int][]int{}
	gold := r.Our.Gold
	towers := 0
	claimed := map[p.Pos]bool{}
	for _, u := range r.Mobiles() {
		economy(r, u, g, c, &oldOut, oldGoals, &gold, claimed, &towers)
	}
	if len(oldOut.Commands) != 0 || len(oldGoals) != 2 {
		t.Fatal("baseline no longer demonstrates crossed travel", oldOut, oldGoals)
	}
	best := 10000
	for _, order := range [][2]int{{0, 1}, {1, 0}} {
		cost := 0
		for i, site := range order {
			path := g.Shortest(g.ID(r.Our.Roles[i].Pos), g.Around([]p.Pos{c.Profiles[r.Our.Type].Weapons[site].Pos}))
			cost = max(cost, len(path)-1)
		}
		best = min(best, cost)
	}
	if best != 0 {
		t.Fatal("fixture lower bound incorrect")
	}
}

func TestEngineAppliesTaskPriority(t *testing.T) {
	r := small()
	r.Map = p.MapInfo{Width: 4, Height: 3, Zones: []p.Zone{{Pos: p.Pos{X: 2, Y: 0}, Type: "task"}}}
	r.Our.Roles[0].Type = "pioneer"
	r.Our.Roles[0].Pos = p.Pos{}
	r.Our.Roles[1].Pos = p.Pos{X: 0, Y: 2}
	timeout := 100
	r.Our.Tasks = []p.PlayerTask{{Pos: p.Pos{X: 2, Y: 0}, Valid: true, Score: 50, Timeout: &timeout}}
	c := DefaultConfig()
	c.Profiles[r.Our.Type] = Profile{Verified: true, Weapons: []Site{{p.Pos{X: 3, Y: 2}, "gatling"}}}
	out, _, tr := (Engine{Config: c}).Decide(t.Context(), r, Memory{})
	found := false
	for _, path := range tr.Paths {
		if path.Result.Objective != "" {
			found = true
			if path.Result.PriorityArrival != 1 || !path.Result.Optimal {
				t.Fatal("priority search did not finish", path)
			}
		}
	}
	if !found || out.Commands["1"].Action != "move" || !near(out.Commands["1"].Targets[0], []p.Pos{{X: 2, Y: 0}}) {
		t.Fatal("task priority not integrated", out, tr)
	}
}

func TestWorkerBudgetDoesNotClaimProof(t *testing.T) {
	r, c := workFixture()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	out := p.Empty()
	tr := Trace{}
	m := Memory{}
	goals := map[int][]int{}
	gold := r.Our.Gold
	assignWorkers(ctx, r, staticGrid(r), c, &m, &out, goals, &gold, &tr)
	if tr.Team == nil || tr.Team.Proven || len(out.Commands) > 0 {
		t.Fatal("canceled search claimed proof or emitted work", tr.Team, out)
	}
}

func TestWorkerAssignmentsReserveSharedGold(t *testing.T) {
	r, c := workFixture()
	r.Our.Gold = 25
	out, _, tr := (Engine{Config: c}).Decide(t.Context(), r, Memory{})
	count := 0
	for _, cmd := range out.Commands {
		if cmd.Action == "build" {
			count++
		}
	}
	if count != 1 || len(tr.Rejected) != 0 {
		t.Fatal("shared gold overcommitted", out, tr)
	}
}

func TestWorkersReserveSharedUpgradeTarget(t *testing.T) {
	r := small()
	r.Our.Gold = 0
	r.Our.Roles[0].Pos = p.Pos{X: 2, Y: 1}
	r.Our.Roles[1].Pos = p.Pos{X: 4, Y: 1}
	for i := range r.Our.Roles {
		r.Our.Roles[i].Backpack = []string{"WeaponUpgradeVoucher1"}
	}
	r.Our.Roles = append(r.Our.Roles, p.Role{ID: 3, Type: "gatling", Health: 1000, Level: 1, Pos: p.Pos{X: 3, Y: 2}})
	out, _, tr := (Engine{Config: DefaultConfig()}).Decide(t.Context(), r, Memory{})
	count := 0
	for _, cmd := range out.Commands {
		if cmd.Action == "use" && cmd.Name == "WeaponUpgradeVoucher1" {
			count++
		}
	}
	if count != 1 || len(tr.Rejected) != 0 {
		t.Fatal("duplicate upgrade work", out, tr)
	}
}

func TestWorkersDoNotDuplicateOneBuildTarget(t *testing.T) {
	r, c := workFixture()
	f := c.Profiles[r.Our.Type]
	f.Weapons = f.Weapons[:1]
	c.Profiles[r.Our.Type] = f
	_, m, _ := (Engine{Config: c}).Decide(t.Context(), r, Memory{})
	count := 0
	for _, work := range m.Work {
		if strings.HasPrefix(work, "build:") {
			count++
		}
	}
	if count != 1 {
		t.Fatal("same construction assigned twice", m.Work)
	}
}

func TestMovementPreservesSafePriorityStep(t *testing.T) {
	r := small()
	r.Our.Roles[0].Type = "pioneer"
	r.Enemy.Roles = []p.Role{{ID: 9, Type: "worker", Health: 100, Pos: p.Pos{X: 4, Y: 3}}}
	g := staticGrid(r)
	roles := r.Mobiles()
	start := nav.State{g.ID(roles[0].Pos), g.ID(roles[1].Pos)}
	goals := [][]int{{g.ID(p.Pos{X: 6, Y: 1})}, {g.ID(p.Pos{X: 6, Y: 6})}}
	out := p.Empty()
	selected := p.Pos{X: 2, Y: 0}
	out.Commands["1"] = p.At("move", selected)
	out.Commands["2"] = p.At("move", p.Pos{X: 4, Y: 2})
	assessMovement(r, g, start, goals, roles, [3]bool{}, false, Memory{}, &out, &Trace{}, 1)
	if out.Commands["1"].Action != "move" || out.Commands["1"].Targets[0] != selected {
		t.Fatal("risk adjustment undid safe priority step", out)
	}
}
