package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
	"fmt"
	"math"
)

type TaskStatistics struct {
	Attempts  int `json:"attempts"`
	Successes int `json:"successes"`
	Rounds    int `json:"rounds"`
}
type TaskAssessment struct {
	Position      p.Pos   `json:"position"`
	SolveRounds   float64 `json:"solveRounds"`
	Success       float64 `json:"success"`
	ExpectedScore float64 `json:"expectedScore"`
	ReturnRounds  int     `json:"returnRounds"`
	CooldownWait  int     `json:"cooldownWait"`
	Value         float64 `json:"value"`
	Safe          bool    `json:"safe"`
}
type LearningAssessment struct {
	DelayLoss      float64 `json:"delayLoss"`
	ExpectedReuses float64 `json:"expectedReuses"`
	FutureGain     float64 `json:"futureGain"`
	Profitable     bool    `json:"profitable"`
}

func siteKey(pos p.Pos) string { return fmt.Sprintf("%d,%d", pos.X, pos.Y) }

// Update once from the engine's last accepted response, before Task is cleared.
// Legal submission without task completion does not count as success.
func observeTaskOutcome(r p.Request, m *Memory) {
	if r.Round != m.Round+1 || m.Task.Text == "" {
		return
	}
	record := func(success bool) {
		if m.TaskStats == nil {
			m.TaskStats = map[string]TaskStatistics{}
		}
		key := siteKey(m.Task.Position)
		s := m.TaskStats[key]
		s.Attempts++
		s.Rounds += max(1, m.Round-m.Task.Start)
		if success {
			s.Successes++
		}
		m.TaskStats[key] = s
	}
	for id, cmd := range m.LastResponse.Commands {
		if cmd.Action != "submitAnswer" {
			continue
		}
		legal, seen := r.Results[id]
		if !seen {
			return
		}
		success := legal && len(r.Errors) == 0 && r.PhaseTask == ""
		if !success && len(r.Errors) == 0 && legal {
			return
		}
		record(success)
		return
	}
	if r.PhaseTask == "" {
		record(false)
	}
}

// Conservative priors until a task point has observations. A skill learned at
// this point only raises the chance of a fast solve; unseen tasks may not match.
func taskEstimate(m Memory, pos p.Pos) (duration, success float64) {
	s := m.TaskStats[siteKey(pos)]
	duration = float64(16+s.Rounds) / float64(2+s.Attempts)
	success = (1.3 + float64(s.Successes)) / float64(2+s.Attempts)
	for _, skill := range m.Skills {
		if !skill.Disabled && skill.Position == pos {
			duration = .5*duration + .5*3
			break
		}
	}
	return max(3, duration), success
}

func defenseCells(r p.Request) []p.Pos {
	var cells []p.Pos
	for _, w := range r.Weapons() {
		cells = append(cells, w.Pos)
	}
	if len(cells) == 0 {
		for _, u := range r.Our.Roles {
			if u.Type == "station" && u.Health > 0 {
				cells = append(cells, u.Cells()...)
			}
		}
	}
	return cells
}
func returnDistance(r p.Request, g nav.Grid, pos p.Pos) int {
	cells := defenseCells(r)
	if len(cells) == 0 {
		return 0
	}
	path := g.Shortest(g.ID(pos), g.Around(cells))
	if len(path) == 0 {
		return 10000
	}
	return len(path) - 1
}

func roleReturnDistance(r p.Request, g nav.Grid, c Config, u p.Role, pos p.Pos) int {
	if w, ok := c.ReturnAssignments[u.ID]; ok {
		path := g.Shortest(g.ID(pos), g.Around(w.Cells()))
		if len(path) == 0 {
			return 10000
		}
		return len(path) - 1
	}
	if c.Strategy.FixedStations {
		kind := "rocket"
		worker := 0
		for _, role := range r.Mobiles() {
			if role.Type == "worker" {
				if role.ID == u.ID {
					kind = "gatling"
					if worker > 0 {
						kind = "railgun"
					}
					break
				}
				worker++
			}
		}
		for _, w := range r.Weapons() {
			if w.Type == kind {
				path := g.Shortest(g.ID(pos), g.Around(w.Cells()))
				if len(path) == 0 {
					return 10000
				}
				return len(path) - 1
			}
		}
	}
	return returnDistance(r, g, pos)
}

func assessTask(r p.Request, g nav.Grid, c Config, m Memory, site p.PlayerTask, travel int, arrival p.Pos) TaskAssessment {
	d, prob := taskEstimate(m, site.Pos)
	timeout := 0
	if site.Timeout != nil {
		timeout = *site.Timeout
	}
	back := returnDistance(r, g, arrival)
	for _, u := range r.Mobiles() {
		if u.Type == "pioneer" {
			back = roleReturnDistance(r, g, c, u, arrival)
			break
		}
	}
	a := TaskAssessment{Position: site.Pos, SolveRounds: d, Success: prob, ReturnRounds: back, CooldownWait: 30}
	// Another available (or soon available) point can absorb this point's cooldown.
	for _, other := range r.Our.Tasks {
		if other.Pos == site.Pos || !other.Valid && other.Cooldown == 0 {
			continue
		}
		path := g.Shortest(g.ID(arrival), g.Around(taskCells(r, other)))
		if len(path) > 0 {
			a.CooldownWait = min(a.CooldownWait, max(0, other.Cooldown-travel-int(math.Ceil(d))-(len(path)-1)))
		}
	}
	// Partial correctness is not exposed numerically: use a documented 25% prior.
	a.ExpectedScore = prob*fullTaskScore(site.Score, timeout, d) + (1-prob)*float64(site.Score)*.25

	goldWeight := .1
	if r.Our.Gold < c.ReserveGold {
		goldWeight = .5
	}
	a.Value = (a.ExpectedScore + goldWeight*float64(site.Gold)*(prob+(1-prob)*.25)) / (float64(travel+1+back) + d)
	a.Safe = a.Value >= c.Strategy.TaskMinExpectedDensity && (r.Day() < 6 || d <= float64(c.Strategy.LateTaskMaxRounds)) && r.Daylight() && !baseEmergency(r, c) && travel+1+int(math.Ceil(d))+back+c.ReturnBuffer+c.Strategy.TaskMinFinishBuffer < r.DayLeft() && travel+1+int(math.Ceil(d)) < 1301-r.Round && (timeout <= 0 || d < float64(timeout))
	return a
}

func learningAssessment(r p.Request, c Config, m *Memory, extra int) *LearningAssessment {
	t := &m.Task
	a := &LearningAssessment{}
	if !learningTime(r, t, extra+1) || baseEmergency(r, c) || !r.Daylight() {
		return a
	}
	for _, s := range m.Skills {
		if !s.Disabled && skillMatches(s.Pattern, t.Text) {
			return a
		}
	}
	d := float64(max(1, r.Round-t.Start))
	a.DelayLoss = 5 * float64(t.Timeout) * (1/d - 1/(d+float64(extra)))
	slow, _ := taskEstimate(*m, t.Position)
	// Task count is not exposed. Cap the horizon estimate at three repeats and
	// use a 60% prior probability of encountering the same family.
	a.ExpectedReuses = .6 * min(3, float64(max(0, 1300-r.Round-extra))/float64(30+int(math.Ceil(slow))))
	a.FutureGain = a.ExpectedReuses * max(0, 5*float64(t.Timeout)*(1.0/3-1/slow))
	a.Profitable = a.FutureGain > a.DelayLoss && extra+c.ReturnBuffer+1 < r.DayLeft()
	return a
}

func baseEmergency(r p.Request, configs ...Config) bool {
	c := DefaultConfig()
	if len(configs) > 0 {
		c = configs[0]
	}
	for _, risk := range assessStructures(r, c) {
		if risk.Critical {
			return true
		}
	}
	return false
}

func shouldRetreat(r p.Request, g nav.Grid, c Config, m Memory, emergency bool) bool {
	if m.Task.Abandon || emergency {
		return true
	}
	if len(defenseCells(r)) == 0 {
		return false
	}
	for _, u := range r.Mobiles() {
		if u.Type != "pioneer" {
			continue
		}
		d, _ := taskEstimate(m, m.Task.Position)
		remaining := max(1, int(math.Ceil(d))-(r.Round-m.Task.Start))
		return !r.Daylight() || remaining+roleReturnDistance(r, g, c, u, u.Pos)+c.ReturnBuffer >= r.DayLeft()
	}
	return false
}

func retreatGoals(r p.Request, g nav.Grid, taskPos p.Pos) []int {
	site := p.PlayerTask{Pos: taskPos}
	cells := taskCells(r, site)
	var goals []int
	for _, id := range g.Around(defenseCells(r)) {
		if !near(g.Pos(id), cells) {
			goals = append(goals, id)
		}
	}
	if len(goals) > 0 {
		return goals
	}
	// If every defensive operating cell overlaps the task, first leave its area.
	for id, blocked := range g.Block {
		if !blocked && !near(g.Pos(id), cells) {
			goals = append(goals, id)
		}
	}
	return goals
}
