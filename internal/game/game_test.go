package game

import (
	p "competition/internal/protocol"
	"context"
	"encoding/json"
	"os"
	"testing"
)

func fixture(t testing.TB) p.Request {
	t.Helper()
	b, e := os.ReadFile("../protocol/testdata/request.json")
	if e != nil {
		t.Fatal(e)
	}
	r, e := p.Decode(b)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func small() p.Request {
	return p.Request{Round: 1, Map: p.MapInfo{Width: 8, Height: 8}, Our: p.Team{ID: "a", Type: "challenger", Gold: 25, Roles: []p.Role{{ID: 1, Type: "worker", Health: 220, Pos: p.Pos{X: 1, Y: 1}}, {ID: 2, Type: "worker", Health: 220, Pos: p.Pos{X: 3, Y: 1}}}}}
}
func TestDeterministicAndMemoryIsolation(t *testing.T) {
	r := fixture(t)
	c := DefaultConfig()
	a := Memory{}
	o, m, _ := (Engine{c}).Decide(context.Background(), r, a)
	o2, m2, _ := (Engine{c}).Decide(context.Background(), r, a)
	b, _ := json.Marshal(o)
	b2, _ := json.Marshal(o2)
	x, _ := json.Marshal(m)
	x2, _ := json.Marshal(m2)
	if string(b) != string(b2) || string(x) != string(x2) {
		t.Fatal("not deterministic")
	}
	if a.Round != 0 || a.News != nil {
		t.Fatal("mutated caller memory")
	}
	if len(o.Commands) == 0 {
		t.Fatal("expected decisions")
	}
}
func TestGoldAndMoveValidation(t *testing.T) {
	r := small()
	c := DefaultConfig()
	c.Profiles["challenger"] = Profile{Verified: true, Weapons: []Site{{p.Pos{X: 1, Y: 2}, "gatling"}, {p.Pos{X: 3, Y: 2}, "rocket"}}}
	out := p.Empty()
	out.Commands["1"] = p.Command{Action: "build", Name: "gatling", Targets: []p.Pos{{X: 1, Y: 2}}}
	out.Commands["2"] = p.Command{Action: "build", Name: "rocket", Targets: []p.Pos{{X: 3, Y: 2}}}
	tr := Trace{}
	v := Validate(r, c, out, &tr)
	if len(v.Commands) != 1 {
		t.Fatalf("shared gold not reserved %+v", v)
	}
	out = p.Empty()
	out.Commands["1"] = p.At("move", p.Pos{X: 2, Y: 1})
	out.Commands["2"] = p.At("move", p.Pos{X: 2, Y: 1})
	v = Validate(r, c, out, &tr)
	if len(v.Commands) != 0 {
		t.Fatal("collision retained")
	}
}
func TestUnverifiedBuildDisabled(t *testing.T) {
	r := small()
	c := DefaultConfig()
	c.Profiles["challenger"] = Profile{Weapons: []Site{{p.Pos{X: 1, Y: 2}, "gatling"}}}
	out := p.Empty()
	out.Commands["1"] = p.Command{Action: "build", Name: "gatling", Targets: []p.Pos{{X: 1, Y: 2}}}
	tr := Trace{}
	v := Validate(r, c, out, &tr)
	if len(v.Commands) != 0 {
		t.Fatal("unverified build allowed")
	}
}
func TestCombatEnergyAndSplash(t *testing.T) {
	r := small()
	r.Robots.Roles = []p.Robot{{ID: 10, Pos: p.Pos{X: 2, Y: 1}, Health: 5}, {ID: 11, Pos: p.Pos{X: 3, Y: 1}, Health: 40}}
	w := p.Role{Type: "railgun", Pos: p.Pos{X: 1, Y: 1}, Level: 3}
	d := shotDamage(r, w, []p.Pos{{X: 4, Y: 1}})
	if d[0] != 5 || d[1] != 25 {
		t.Fatal(d)
	}
	w.Type = "rocket"
	d = shotDamage(r, w, []p.Pos{{X: 2, Y: 1}, {X: 2, Y: 1}})
	if d[0] != 40 || d[1] != 20 {
		t.Fatal(d)
	}
	if cone(p.Pos{}, []p.Pos{{X: 1, Y: 0}, {X: -1, Y: 1}}) {
		t.Fatal("bad cone")
	}
}
func TestControllerCannotMoveAndShoot(t *testing.T) {
	r := small()
	r.Round = 71
	r.Our.Roles = append(r.Our.Roles, p.Role{ID: 3, Type: "gatling", Health: 1000, Level: 1, Pos: p.Pos{X: 1, Y: 2}})
	out := p.Empty()
	out.Commands["1"] = p.At("move", p.Pos{X: 2, Y: 1})
	out.Commands["3"] = p.Command{Action: "attack", Controller: "1", Targets: []p.Pos{{X: 2, Y: 3}}}
	tr := Trace{}
	v := Validate(r, DefaultConfig(), out, &tr)
	if _, ok := v.Commands["3"]; ok {
		t.Fatal("double role action")
	}
}
func TestTaskPipeline(t *testing.T) {
	r := small()
	r.Our.Roles[0].Type = "pioneer"
	r.PhaseTask = "Return the result"
	c := DefaultConfig()
	c.Strategy.ParallelTask = false // Retain coverage of configurable serial tool execution.
	m := Memory{}
	tr := Trace{}
	out := p.Empty()
	if !taskTurn(r, r.Our.Roles[0], c, &m, &out, &tr) || out.Prompt == "" {
		t.Fatal("missing prompt")
	}
	r.Round++
	r.LLM = `{"session":"` + m.Task.Session + `","command":"printf 42"}`
	out = p.Empty()
	taskTurn(r, r.Our.Roles[0], c, &m, &out, &tr)
	if out.Execute != "printf 42" || out.Prompt != "" {
		t.Fatal(out)
	}
	r.Round++
	r.CmdResult = "[exitCode:0]\n42"
	out = p.Empty()
	taskTurn(r, r.Our.Roles[0], c, &m, &out, &tr)
	if out.Prompt == "" || m.Task.LastTool != r.CmdResult {
		t.Fatal("tool not consumed")
	}
	r.Round++
	r.LLM = `{"session":"` + m.Task.Session + `","answer":"42"}`
	out = p.Empty()
	taskTurn(r, r.Our.Roles[0], c, &m, &out, &tr)
	if out.Commands["1"].Answer == nil || *out.Commands["1"].Answer != "42" {
		t.Fatal(out)
	}
}
func TestOldTaskReplyRejected(t *testing.T) {
	r := small()
	r.PhaseTask = "new task"
	r.Round = 4
	m := Memory{Task: TaskMemory{Text: "old", Session: "old", Pending: Pending{"llm", 3, "old"}}}
	r.LLM = `{"session":"old","answer":"wrong"}`
	out := p.Empty()
	tr := Trace{}
	taskTurn(r, r.Our.Roles[0], DefaultConfig(), &m, &out, &tr)
	if len(out.Commands) != 0 {
		t.Fatal("old reply submitted")
	}
}

func TestRecipeSkipsLLMRoundTrip(t *testing.T) {
	r := small()
	r.Our.Roles[0].Type = "pioneer"
	r.PhaseTask = "add 4 8"
	c := DefaultConfig()
	c.Recipes = []Recipe{{Name: "add", Pattern: `^add (?P<a>[0-9]+) (?P<b>[0-9]+)$`, Python: "print('example')"}}
	m := Memory{}
	out := p.Empty()
	tr := Trace{}
	taskTurn(r, r.Our.Roles[0], c, &m, &out, &tr)
	if out.Execute == "" || out.Prompt != "" {
		t.Fatal("recipe should bypass model")
	}
	r.Round++
	r.CmdResult = "[exitCode:0]\n{\"answer\":\"12\"}"
	out = p.Empty()
	taskTurn(r, r.Our.Roles[0], c, &m, &out, &tr)
	if out.Commands["1"].Answer == nil || *out.Commands["1"].Answer != "12" {
		t.Fatal("recipe result not submitted")
	}
}
func TestBuildNeverWalksOntoSite(t *testing.T) {
	r := small()
	u := r.Our.Roles[0]
	site := p.Pos{X: 5, Y: 5}
	g := staticGrid(r)
	out := p.Empty()
	goals := map[int][]int{}
	cmd := p.At("build", site)
	cmd.Name = "rocket"
	moveOr(r, u, g, []p.Pos{site}, cmd, &out, goals)
	for _, id := range goals[u.ID] {
		if g.Pos(id) == site {
			t.Fatal("construction goal includes building cell")
		}
	}
}
func TestTaskEndClearsSameTextSession(t *testing.T) {
	r := small()
	m := Memory{Team: r.Our.ID, Side: r.Our.Type, Task: TaskMemory{Session: "old", Text: "same", Start: 1}}
	tr := Trace{}
	updateMemory(r, &m, &tr)
	if m.Task.Text != "" {
		t.Fatal("stale task session")
	}
}
func BenchmarkDecideFixture(b *testing.B) {
	r := fixture(b)
	e := Engine{DefaultConfig()}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Decide(context.Background(), r, Memory{})
	}
}
