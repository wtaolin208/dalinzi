package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
	"testing"
)

func movementFixture() p.Request {
	r := small()
	r.Our.Gold = 0
	r.Our.Roles = []p.Role{{ID: 1, Type: "worker", Health: 220, Pos: p.Pos{X: 1, Y: 2}}}
	return r
}

func planStep(r p.Request, g nav.Grid, m Memory) (p.Response, Trace) {
	out := p.Empty()
	out.Commands["1"] = p.At("move", p.Pos{X: 2, Y: 2})
	tr := Trace{}
	roles := r.Mobiles()
	start := nav.State{}
	goals := make([][]int, len(roles))
	locked := [3]bool{}
	for i, u := range roles {
		start[i] = g.ID(u.Pos)
		goals[i] = []int{g.ID(p.Pos{X: 5, Y: 2})}
		if i > 0 {
			locked[i] = true
			goals[i] = []int{start[i]}
		}
	}
	assessMovement(r, g, start, goals, roles, locked, false, m, &out, &tr)
	return out, tr
}

func TestMovementAvoidsReachableCellAndPreservesActions(t *testing.T) {
	r := movementFixture()
	r.Enemy.Roles = []p.Role{{ID: 9, Type: "worker", Health: 100, Pos: p.Pos{X: 2, Y: 1}}}
	g := staticGrid(r)
	out, tr := planStep(r, g, Memory{})
	if out.Commands["1"].Targets[0] != (p.Pos{X: 2, Y: 3}) {
		t.Fatalf("did not choose safe equal-length route: %+v", out)
	}
	if len(tr.Movement) != 1 || tr.Movement[0].Risk != 0 {
		t.Fatal(tr)
	}
	// The same safe cell is occupied by a teammate who must stay put.
	r.Our.Roles = append(r.Our.Roles, p.Role{ID: 2, Type: "worker", Health: 100, Pos: p.Pos{X: 2, Y: 3}})
	out, _ = planStep(r, staticGrid(r), Memory{})
	if cmd, ok := out.Commands["1"]; ok && cmd.Targets[0] == r.Our.Roles[1].Pos {
		t.Fatal("moved into locked teammate")
	}
	if _, ok := out.Commands["2"]; ok {
		t.Fatal("moved locked teammate")
	}
}

func TestMovementRiskIsSoftAndOneStep(t *testing.T) {
	r := movementFixture()
	r.Robots.Roles = []p.Robot{{ID: 9, Health: 100, Pos: p.Pos{X: 2, Y: 1}}}
	g := staticGrid(r)
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			g.Block[g.ID(p.Pos{X: x, Y: y})] = y != 2
		}
	}
	out, _ := planStep(r, g, Memory{})
	if out.Commands["1"].Action != "move" || out.Commands["1"].Targets[0] != (p.Pos{X: 2, Y: 2}) {
		t.Fatal("risk blocked only useful route", out)
	}
	risk := movementRisk(r, g)
	if risk[g.ID(p.Pos{X: 4, Y: 2})] != 0 {
		t.Fatal("risk extended beyond one step")
	}
	// Static enemy buildings and dead units do not generate movement risk.
	r.Robots.Roles = nil
	r.Enemy.Roles = []p.Role{{Type: "station", Health: 100, Pos: p.Pos{X: 2, Y: 1}}, {Type: "worker", Health: 0, Pos: p.Pos{X: 3, Y: 1}}}
	for _, v := range movementRisk(r, g) {
		if v != 0 {
			t.Fatal("immobile unit generated risk")
		}
	}
}

func TestRepeatedBlockageDetoursAndResets(t *testing.T) {
	r := movementFixture()
	r.Round = 3
	m := Memory{Team: r.Our.ID, Side: r.Our.Type, Round: 2, Stuck: map[int]int{1: 1}, LastPositions: map[int]p.Pos{1: r.Our.Roles[0].Pos}, LastResponse: p.Empty()}
	m.LastResponse.Commands["1"] = p.At("move", p.Pos{X: 2, Y: 2})
	updateMemory(r, &m, &Trace{})
	if m.Stuck[1] != 2 {
		t.Fatal(m.Stuck)
	}
	out, tr := planStep(r, staticGrid(r), m)
	cmd := out.Commands["1"]
	if cmd.Action != "move" || cmd.Targets[0] == (p.Pos{X: 2, Y: 2}) {
		t.Fatal("retried blocked cell instead of detouring", out)
	}
	if tr.Movement[0].Stuck != 2 {
		t.Fatal(tr)
	}
	m.Round = 3
	m.LastResponse = out
	r.Round = 4
	r.Our.Roles[0].Pos = cmd.Targets[0]
	updateMemory(r, &m, &Trace{})
	if m.Stuck[1] != 0 {
		t.Fatal("successful move did not reset streak")
	}
	// A skipped snapshot cannot establish consecutive failed moves.
	m.Stuck[1] = 2
	r.Round = 8
	updateMemory(r, &m, &Trace{})
	if m.Stuck[1] != 0 {
		t.Fatal("gap retained stale failure streak")
	}
}

func TestEngineUsesBlockageAssessment(t *testing.T) {
	r := movementFixture()
	r.Map.Zones = []p.Zone{{Type: "stone", Pos: p.Pos{X: 5, Y: 2}}}
	e := Engine{Config: DefaultConfig()}
	first, memory, _ := e.Decide(t.Context(), r, Memory{})
	if first.Commands["1"].Action != "move" {
		t.Fatal(first)
	}
	r.Round++ // Judge leaves position unchanged after the first attempted move.
	_, memory, _ = e.Decide(t.Context(), r, memory)
	r.Round++ // A second failure must activate detouring through the actual engine.
	out, after, tr := e.Decide(t.Context(), r, memory)
	if after.Stuck[1] != 2 || len(tr.Movement) == 0 {
		t.Fatal("assessment not wired into engine", after.Stuck, tr)
	}
	if out.Commands["1"].Action != "move" || out.Commands["1"].Targets[0] == memory.LastResponse.Commands["1"].Targets[0] {
		t.Fatal("engine did not detour", out)
	}
	if len(tr.Rejected) != 0 {
		t.Fatal("detour failed validation", tr.Rejected)
	}
}
