package game

import (
	p "competition/internal/protocol"
	"math"
	"testing"
)

func TestTaskSelectionUsesSpeedBonusAndHistory(t *testing.T) {
	r := small()
	r.Our.Roles = r.Our.Roles[:1]
	r.Our.Roles[0].Type = "pioneer"
	r.Our.Gold = 100
	short, long := 20, 100
	r.Our.Tasks = []p.PlayerTask{{Pos: p.Pos{X: 2, Y: 1}, Valid: true, Score: 50, Timeout: &short}, {Pos: p.Pos{X: 1, Y: 2}, Valid: true, Score: 50, Timeout: &long}}
	_, m, tr := (Engine{Config: DefaultConfig()}).Decide(t.Context(), r, Memory{})
	if m.Task.Position != r.Our.Tasks[1].Pos || len(tr.Tasks) != 2 || tr.Tasks[1].ExpectedScore <= tr.Tasks[0].ExpectedScore {
		t.Fatal("speed bonus not used", m.Task, tr.Tasks)
	}
	// Large rewards do not compensate for a task point with repeated failures.
	m = Memory{TaskStats: map[string]TaskStatistics{siteKey(r.Our.Tasks[1].Pos): {Attempts: 20, Successes: 0, Rounds: 400}}}
	g := staticGrid(r)
	a := assessTask(r, g, DefaultConfig(), m, r.Our.Tasks[0], 0, r.Our.Roles[0].Pos)
	b := assessTask(r, g, DefaultConfig(), m, r.Our.Tasks[1], 0, r.Our.Roles[0].Pos)
	if a.Value <= b.Value {
		t.Fatal("failures do not change preference", a, b)
	}
}

func TestTaskAdmissionReservesReturnTime(t *testing.T) {
	r := small()
	r.Round = 60
	timeout := 100
	site := p.PlayerTask{Pos: p.Pos{X: 6, Y: 6}, Valid: true, Score: 1000, Timeout: &timeout}
	r.Our.Tasks = []p.PlayerTask{site}
	r.Our.Roles = append(r.Our.Roles, p.Role{ID: 3, Type: "gatling", Health: 1000, Pos: p.Pos{X: 1, Y: 2}})
	a := assessTask(r, staticGrid(r), DefaultConfig(), Memory{}, site, 4, p.Pos{X: 5, Y: 5})
	if a.Safe {
		t.Fatal("accepted task without time to return", a)
	}
	r.Round = 1
	if !assessTask(r, staticGrid(r), DefaultConfig(), Memory{}, site, 4, p.Pos{X: 5, Y: 5}).Safe {
		t.Fatal("rejected safe daytime task")
	}
}

func TestTaskCooldownAndGoldAreNotRawPoints(t *testing.T) {
	r := small()
	r.Our.Gold = 100
	site := p.PlayerTask{Pos: p.Pos{X: 2, Y: 2}, Valid: true, Score: 50, Gold: 100}
	g := staticGrid(r)
	c := DefaultConfig()
	one := assessTask(r, g, c, Memory{}, site, 1, r.Our.Roles[0].Pos)
	r.Our.Tasks = []p.PlayerTask{site, {Pos: p.Pos{X: 5, Y: 5}, Cooldown: 10}}
	two := assessTask(r, g, c, Memory{}, site, 1, r.Our.Roles[0].Pos)
	if two.CooldownWait >= one.CooldownWait || two.Value != one.Value {
		t.Fatal("cooldown forecast must not dilute current task density", one, two)
	}
	r.Our.Gold = 0
	poor := assessTask(r, g, c, Memory{}, site, 1, r.Our.Roles[0].Pos)
	if poor.ExpectedScore != two.ExpectedScore || poor.Value <= two.Value {
		t.Fatal("gold marginal utility incorrect", poor, two)
	}
}

func TestLearningInvestmentUsesDelayLoss(t *testing.T) {
	r := learningRequest()
	r.Round = 5
	m := Memory{Task: TaskMemory{Text: r.PhaseTask, Start: 1, Timeout: 100}}
	a := learningAssessment(r, DefaultConfig(), &m, 2)
	if math.Abs(a.DelayLoss-(175-133.3333333333333)) > 1e-6 || !a.Profitable {
		t.Fatal("wrong learning investment", a)
	}
	r.Round = 2 // Already solvable one round after acceptance: validation is expensive.
	a = learningAssessment(r, DefaultConfig(), &m, 1)
	if a.Profitable || a.DelayLoss != 250 {
		t.Fatal("fast answer should not be delayed", a)
	}
	r.Round = 1235
	m.Task.Start = 1231
	if learningAssessment(r, DefaultConfig(), &m, 2).Profitable {
		t.Fatal("late learning should be skipped")
	}
}

func TestLearningUnprofitableAnswerSubmittedImmediately(t *testing.T) {
	r := learningRequest()
	r.Round = 2
	m := Memory{Task: TaskMemory{Session: "s", Text: r.PhaseTask, Start: 1, Timeout: 100, Pending: Pending{"llm", 1, "s"}}}
	answer := "12"
	r.LLM = modelReply(t, ToolPlan{Session: "s", Answer: &answer, Skill: sampleSkill()})
	out := p.Empty()
	tr := Trace{}
	taskTurn(r, r.Our.Roles[0], DefaultConfig(), &m, &out, &tr)
	if out.Execute != "" || out.Prompt != "" || out.Commands["1"].Answer == nil || tr.Learning == nil || tr.Learning.Profitable {
		t.Fatal("unprofitable learning delayed submission", out, tr)
	}
}

func TestEmergencyAbandonsTaskAndActuallyMoves(t *testing.T) {
	r := learningRequest()
	r.Round = 75
	r.Our.Roles[0].Pos = p.Pos{X: 6, Y: 5}
	r.Our.Roles = append(r.Our.Roles, p.Role{ID: 2, Type: "station", Health: 100, Pos: p.Pos{X: 0, Y: 1}}, p.Role{ID: 3, Type: "gatling", Health: 1000, Pos: p.Pos{X: 2, Y: 2}})
	r.Robots.Roles = []p.Robot{{ID: 10, Type: "smallRobot", Health: 40, Pos: p.Pos{X: 0, Y: 3}}}
	m := Memory{Team: r.Our.ID, Side: r.Our.Type, Round: 74, Task: TaskMemory{Text: r.PhaseTask, Position: p.Pos{X: 6, Y: 6}, Start: 70, Timeout: 100, Session: "s", Pending: Pending{"llm", 74, "s"}}}
	answer := "12"
	r.LLM = modelReply(t, ToolPlan{Session: "s", Answer: &answer})
	out, after, tr := (Engine{Config: DefaultConfig()}).Decide(t.Context(), r, m)
	if !after.Task.Abandon || out.Execute != "" || out.Prompt != "" || out.Commands["1"].Action != "move" {
		t.Fatal("pioneer still locked in task", out, after.Task, tr)
	}
	if near(out.Commands["1"].Targets[0], []p.Pos{m.Task.Position}) {
		t.Fatal("did not leave task area", out)
	}
	if len(tr.Rejected) != 0 {
		t.Fatal("retreat invalid", tr.Rejected)
	}
}

func TestDawnPrefersKillsOverUnfinishedDamage(t *testing.T) {
	r := small()
	r.Our.Roles = nil
	r.Robots.Roles = []p.Robot{{ID: 1, Type: "smallRobot", Health: 10}, {ID: 2, Type: "largeRobot", Health: 500}}
	r.Round = 100
	if damageValue(r, []int{0, 40}) <= 0 || damageValue(r, []int{0, 40}) >= damageValue(r, []int{10, 0}) {
		t.Fatal("safe fire must prefer efficient kills while preserving future damage value")
	}
	r.Round = 130
	if damageValue(r, []int{0, 40}) != 0 || damageValue(r, []int{10, 0}) <= 0 {
		t.Fatal("dawn still rewards unfinished damage")
	}
}

func TestRetreatBeforeNightPreservesSafeTask(t *testing.T) {
	r := learningRequest()
	r.Our.Roles = append(r.Our.Roles, p.Role{ID: 3, Type: "gatling", Health: 1000, Pos: p.Pos{X: 6, Y: 6}})
	m := Memory{Task: TaskMemory{Text: r.PhaseTask, Start: 1}}
	r.Round = 20
	if shouldRetreat(r, staticGrid(r), DefaultConfig(), m, false) {
		t.Fatal("safe daytime task abandoned")
	}
	r.Round = 66
	if !shouldRetreat(r, staticGrid(r), DefaultConfig(), m, false) {
		t.Fatal("return buffer ignored")
	}
}

func TestTaskStatisticsJudgeCorrelation(t *testing.T) {
	r := learningRequest()
	r.Round = 6
	r.PhaseTask = ""
	r.Results = map[string]bool{"1": true}
	m := Memory{Round: 5, Team: r.Our.ID, Side: r.Our.Type, Task: TaskMemory{Text: "add 4 8", Start: 1, Position: p.Pos{X: 2, Y: 2}}, LastResponse: p.Empty()}
	answer := "12"
	m.LastResponse.Commands["1"] = p.Command{Action: "submitAnswer", Answer: &answer}
	updateMemory(r, &m, &Trace{})
	s := m.TaskStats["2,2"]
	if s.Attempts != 1 || s.Successes != 1 || s.Rounds != 4 || m.Task.Text != "" {
		t.Fatal("outcome not recorded before clearing task", m)
	}
	// Duplicate update after task clearing must not count a second sample.
	updateMemory(r, &m, &Trace{})
	if m.TaskStats["2,2"] != s {
		t.Fatal("duplicate outcome counted")
	}
}
