package game

import (
	p "competition/internal/protocol"
	"encoding/json"
	"strings"
	"testing"
)

func sampleSkill() *SkillProposal {
	return &SkillProposal{Recipe: Recipe{Name: "addition", Pattern: `^add (?P<a>[0-9]+) (?P<b>[0-9]+)$`, Python: "import json,sys\np=json.loads(sys.argv[1])\nprint(json.dumps({'answer':str(int(p['a'])+int(p['b']))}))"}, SOP: "Extract a and b, add integers, output answer JSON."}
}

func learningRequest() p.Request {
	r := small()
	r.Our.Roles = r.Our.Roles[:1]
	r.Our.Roles[0].Type = "pioneer"
	r.Our.Gold = 0
	r.PhaseTask = "add 4 8"
	limit := 100
	r.Our.Tasks = []p.PlayerTask{{Timeout: &limit}}
	return r
}

func modelReply(t *testing.T, plan ToolPlan) string {
	t.Helper()
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSkillLearningEngineLifecycle(t *testing.T) {
	r := learningRequest()
	e := Engine{Config: DefaultConfig()}
	e.Config.Strategy.SubmitFirst = false // Legacy validation-before-submit mode remains available.
	_, m, _ := e.Decide(t.Context(), r, Memory{})
	r.Round++
	answer := "12"
	r.LLM = modelReply(t, ToolPlan{Session: m.Task.Session, Answer: &answer, Skill: sampleSkill()})
	out, m, _ := e.Decide(t.Context(), r, m)
	if out.Execute == "" || out.Commands["1"].Action == "submitAnswer" || len(m.Skills) != 0 {
		t.Fatal("candidate not held for validation", out, m.Skills)
	}
	if m.Task.Pending.Kind != "skill_validation" {
		t.Fatal(m.Task)
	}
	r.Round++
	r.LLM = ""
	r.CmdResult = "[exitCode:0]\n{\"answer\":\"12\"}"
	out, m, _ = e.Decide(t.Context(), r, m)
	if out.Commands["1"].Answer == nil || *out.Commands["1"].Answer != "12" || m.SkillReceipt == nil || len(m.Skills) != 0 {
		t.Fatal("must submit before admission", out)
	}
	r.Round++
	r.PhaseTask = ""
	r.Results = map[string]bool{"1": true}
	r.CmdResult = ""
	_, m, _ = e.Decide(t.Context(), r, m)
	if len(m.Skills) != 1 || m.Skills[0].Disabled || m.Task.Text != "" {
		t.Fatal("skill not retained across task completion", m)
	}
	// Serialization is the same path used by recording/replay; it must retain
	// the skill without aliases to the caller's previous memory.
	copy := CloneMemory(m)
	copy.Skills[0].SOP = "changed"
	if m.Skills[0].SOP == "changed" {
		t.Fatal("skill memory aliased")
	}
	r.Round++
	r.PhaseTask = "add 7 9"
	r.Results = nil
	out, m, _ = e.Decide(t.Context(), r, m)
	if out.Execute == "" || out.Prompt != "" || m.Task.Recipe != m.Skills[0].Name {
		t.Fatal("new parameters did not reuse skill", out)
	}
	r.Round++
	r.CmdResult = "[exitCode:0]\n{\"answer\":\"16\"}"
	out, m, _ = e.Decide(t.Context(), r, m)
	if out.Commands["1"].Answer == nil || *out.Commands["1"].Answer != "16" {
		t.Fatal(out)
	}
	r.Round++
	r.Errors = []p.Error{{Code: 2, Description: "wrong answer"}}
	r.Results = map[string]bool{"1": true}
	out, m, _ = e.Decide(t.Context(), r, m)
	if !m.Skills[0].Disabled || out.Prompt == "" {
		t.Fatal("judge failure must disable and fall back", out, m.Skills)
	}
}

func TestSkillValidationFailuresStillSubmitAnswer(t *testing.T) {
	for _, result := range []string{"[exitCode:0]\n{\"answer\":\"wrong\"}", "[exitCode:1]\nerror", "[TIMEOUT]\npartial", "[exitCode:0]\n{\"answer\":\"12\"}\n[TRUNCATED]"} {
		r := learningRequest()
		r.Round = 5
		r.CmdResult = result
		m := Memory{Task: TaskMemory{Session: "s", Text: r.PhaseTask, Answer: "12", Candidate: sampleSkill(), Pending: Pending{"skill_validation", 4, "s"}}}
		out := p.Empty()
		taskTurn(r, r.Our.Roles[0], DefaultConfig(), &m, &out, &Trace{})
		if m.SkillReceipt != nil || len(m.Skills) != 0 || out.Commands["1"].Answer == nil || *out.Commands["1"].Answer != "12" {
			t.Fatal("failed validation promoted or lost answer", result, out)
		}
	}
}

func TestSkillAdmissionRequiresCorrelatedJudgeFeedback(t *testing.T) {
	for _, mode := range []string{"wrong", "missing", "late", "still_active", "illegal", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			r := learningRequest()
			r.Round = 6
			r.PhaseTask = ""
			r.Results = map[string]bool{"1": true}
			m := Memory{SkillReceipt: &SkillReceipt{Round: 5, Role: "1", Task: "add 4 8", Candidate: sampleSkill()}}
			switch mode {
			case "wrong":
				r.Errors = []p.Error{{Code: 2}}
			case "missing":
				r.Results = nil
			case "late":
				r.Round = 7
			case "still_active":
				r.PhaseTask = "add 4 8"
			case "illegal":
				r.Results["1"] = false
			case "timeout":
				r.Errors = []p.Error{{Code: 1}}
			}
			settleSkill(r, &m, &Trace{})
			if len(m.Skills) != 0 || m.SkillReceipt != nil {
				t.Fatal("uncertain result admitted", m)
			}
		})
	}
}

func TestSkillDistillationAndDeadline(t *testing.T) {
	r := learningRequest()
	r.Round = 5
	m := Memory{Task: TaskMemory{Session: "s", Text: r.PhaseTask, Start: 1, Timeout: 100, LastTool: "[exitCode:0]\n12", Pending: Pending{"llm", 4, "s"}}}
	answer := "12"
	r.LLM = modelReply(t, ToolPlan{Session: "s", Answer: &answer})
	out := p.Empty()
	taskTurn(r, r.Our.Roles[0], DefaultConfig(), &m, &out, &Trace{})
	if m.Task.Pending.Kind != "skill_distill" || !strings.Contains(out.Prompt, "SOP") {
		t.Fatal("no automatic distillation", out)
	}
	r.Round++
	r.LLM = modelReply(t, ToolPlan{Session: "s", Skill: sampleSkill()})
	out = p.Empty()
	taskTurn(r, r.Our.Roles[0], DefaultConfig(), &m, &out, &Trace{})
	if out.Execute == "" || m.Task.Pending.Kind != "skill_validation" {
		t.Fatal("distilled skill not tested")
	}
	// Near deadline, learning must not delay submission.
	m.Task.Pending = Pending{"llm", r.Round - 1, "s"}
	m.Task.Start = 1
	m.Task.Timeout = r.Round + 1
	r.Our.Tasks = nil
	r.LLM = modelReply(t, ToolPlan{Session: "s", Answer: &answer, Skill: sampleSkill()})
	out = p.Empty()
	taskTurn(r, r.Our.Roles[0], DefaultConfig(), &m, &out, &Trace{})
	if out.Execute != "" || out.Commands["1"].Answer == nil {
		t.Fatal("deadline delayed by learning", out)
	}
}

func TestSkillRejectsMalformedAndStaleCandidates(t *testing.T) {
	r := learningRequest()
	r.Round = 5
	for _, pattern := range []string{"[", `add (?P<a>.*)`, "^add 4 8$", `^other (?P<a>.*)$`} {
		s := sampleSkill()
		s.Pattern = pattern
		task := TaskMemory{Session: "s", Text: r.PhaseTask, Start: 1, Timeout: 100, Answer: "12"}
		out := p.Empty()
		if startSkillValidation(r, DefaultConfig(), &task, s, &out, &Trace{}) || out.Execute != "" {
			t.Fatal("invalid candidate accepted", pattern)
		}
	}
	m := Memory{Task: TaskMemory{Session: "new", Text: r.PhaseTask, Answer: "12", Pending: Pending{"skill_distill", 4, "new"}}}
	r.LLM = modelReply(t, ToolPlan{Session: "old", Skill: sampleSkill()})
	out := p.Empty()
	taskTurn(r, r.Our.Roles[0], DefaultConfig(), &m, &out, &Trace{})
	if out.Execute != "" || m.SkillReceipt != nil || out.Commands["1"].Answer == nil {
		t.Fatal("stale learning response used", out)
	}
}

func TestLearnedSkillRuntimeFailureAndReset(t *testing.T) {
	r := learningRequest()
	r.Round = 5
	r.CmdResult = "[TIMEOUT]\n"
	s := LearnedSkill{SkillProposal: *sampleSkill()}
	m := Memory{Team: r.Our.ID, Side: r.Our.Type, Round: 4, Skills: []LearnedSkill{s}, Task: TaskMemory{Session: "s", Text: r.PhaseTask, Recipe: s.Name, Attempts: 1, Pending: Pending{"command", 4, "s"}}}
	out := p.Empty()
	taskTurn(r, r.Our.Roles[0], DefaultConfig(), &m, &out, &Trace{})
	if !m.Skills[0].Disabled || out.Prompt == "" || out.Execute != "" {
		t.Fatal("runtime failure did not fall back", out)
	}
	r.Our.Type = "defender"
	updateMemory(r, &m, &Trace{})
	if len(m.Skills) != 0 {
		t.Fatal("skills leaked across half/environment reset")
	}
}
