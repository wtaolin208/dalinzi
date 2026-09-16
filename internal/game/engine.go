package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
	"context"
	"fmt"
	"strconv"
)

type PathTrace struct {
	Roles      []int      `json:"roles"`
	Goals      [][]int    `json:"goals"`
	Result     nav.Result `json:"result"`
	Assignment []int      `json:"assignment,omitempty"`
}
type Trace struct {
	DayPlan         *DayPlan            `json:"dayPlan,omitempty"`
	Score           *ScoreAssessment    `json:"score,omitempty"`
	Strategy        *BattlePlan         `json:"strategy,omitempty"`
	ItemDamage      []int               `json:"itemDamage,omitempty"`
	Mode            string              `json:"mode"`
	Notes           []string            `json:"notes"`
	Paths           []PathTrace         `json:"paths"`
	Rejected        map[string]string   `json:"rejected"`
	PredictedDamage []int               `json:"predictedDamage,omitempty"`
	CombatValue     float64             `json:"combatValue"`
	MapHypothesis   bool                `json:"mapHypothesis"`
	Movement        []MoveAssessment    `json:"movement,omitempty"`
	Tasks           []TaskAssessment    `json:"tasks,omitempty"`
	Learning        *LearningAssessment `json:"learning,omitempty"`
	Team            *WorkTrace          `json:"team,omitempty"`
}
type Engine struct{ Config Config }

func (e Engine) Decide(ctx context.Context, r p.Request, before Memory) (p.Response, Memory, Trace) {
	m := CloneMemory(before)
	tr := Trace{Rejected: map[string]string{}, MapHypothesis: e.Config.ExperimentalDemoLayout}
	updateMemory(r, &m, &tr)
	observeNightRisk(r, e.Config, &m)
	if repeatedNightRisk(r, m) {
		e.Config.Strategy.EnableOffense = false
		e.Config.Strategy.TreasureConfidence = max(e.Config.Strategy.TreasureConfidence, .95)
		tr.Notes = append(tr.Notes, "two risky nights: suspend pressure and low-confidence treasure")
	}
	score := assessScore(r, e.Config)
	day := planDay(r, e.Config)
	tr.Score, tr.DayPlan = &score, &day
	out := p.Empty()
	g := staticGrid(r)
	roles := r.Mobiles()
	goals := map[int][]int{}
	emergency := false
	for _, risk := range assessStructures(r, e.Config) {
		emergency = emergency || risk.Critical
	}
	if r.PhaseTask != "" && shouldRetreat(r, g, e.Config, m, emergency) {
		m.Task.Abandon = true
		m.Task.Pending = Pending{}
		m.Task.Candidate = nil
		tr.Notes = append(tr.Notes, "task abandoned for defense: leaving task area")
	}
	planning := r
	if m.Task.Abandon {
		planning.PhaseTask = ""
	}
	pairs, paths := defense(ctx, planning, g, e.Config)
	tr.Paths = append(tr.Paths, paths...)
	plan := assessFSM(ctx, r, g, e.Config, m, pairs)
	tr.Strategy = &plan
	if plan.State == StateEnd {
		m.StrategyState = StateEnd
		m.Round = r.Round
		m.LastResponse = out
		m.Task.Pending = Pending{}
		m.NewsPending = Pending{}
		return out, m, tr
	}
	policy := plan.Policy
	if policy.Return && r.PhaseTask != "" && !m.Task.Abandon {
		m.Task.Abandon = true
		m.Task.Pending = Pending{}
		m.Task.Candidate = nil
	}

	defending := policy.Return || policy.Emergency || policy.Fire
	tr.Mode = "economy"
	if defending {
		tr.Mode = "defense"
	}
	if len(profile(r, e.Config).Weapons) == 0 {
		tr.Notes = append(tr.Notes, "no verified build profile: building disabled; supply profiles or explicitly opt into experimental demo layout")
	}
	for _, u := range roles {
		if policy.Task && u.Type == "pioneer" && r.PhaseTask != "" && !m.Task.Abandon {
			if taskTurn(r, u, e.Config, &m, &out, &tr) {
				goals[u.ID] = []int{g.ID(u.Pos)}
			}
		}
		if u.Type == "pioneer" && m.Task.Abandon {
			goals[u.ID] = retreatGoals(r, g, m.Task.Position)
		}
	}
	if policy.Emergency || policy.Fire {
		emergencyItems(r, e.Config, plan, pairs, &out, &tr)
		combatPairs := make([]Pair, 0, len(pairs))
		for _, pair := range pairs {
			if !m.Task.Abandon || pair.Role.Type != "pioneer" {
				combatPairs = append(combatPairs, pair)
			}
		}
		if policy.Fire {
			fight(r, combatPairs, e.Config, m, &out, &tr)
		}
	}
	gold := r.Our.Gold
	for _, u := range roles {
		key := strconv.Itoa(u.ID)
		if _, ok := out.Commands[key]; ok {
			continue
		}
		controlling := false
		for _, cmd := range out.Commands {
			if cmd.Controller == key {
				controlling = true
			}
		}
		if controlling {
			goals[u.ID] = []int{g.ID(u.Pos)}
			continue
		}
		if _, locked := goals[u.ID]; locked {
			continue
		}
		if (policy.Emergency || !policy.Return || !r.Daylight()) && (u.Type != "worker" || defending) && useOwned(r, u, &out) {
			goals[u.ID] = []int{g.ID(u.Pos)}
			continue
		}
		_, mustReturn := plan.ReturnRoles[u.ID]
		if policy.Return || emergency || !r.Daylight() || mustReturn || m.Task.Abandon {
			for _, pair := range pairs {
				if pair.Role.ID == u.ID {
					goals[u.ID] = g.Around([]p.Pos{pair.Weapon.Pos})
				}
			}
			if _, ok := goals[u.ID]; !ok {
				goals[u.ID] = g.Around(defenseCells(r))
			}
			continue
		}
		if policy.Task && u.Type == "pioneer" && e.Config.EnableTasks {
			budget := max(0, gold-constructionReserve(r, e.Config))
			beforeBudget := budget
			acted := pioneer(r, u, g, e.Config, &m, &out, goals, &budget, &tr)
			gold -= beforeBudget - budget
			if acted {
				continue
			}
		}
	}
	if policy.Summon {
		offenseTurn(r, g, e.Config, &m, plan, &out, goals, &gold)
	}
	if policy.Work {
		assignWorkers(ctx, r, g, e.Config, &m, &out, goals, &gold, &tr)
	}
	// New buildings already reserved this turn must also constrain every route.
	for _, cmd := range out.Commands {
		if cmd.Action == "build" && len(cmd.Targets) == 1 {
			q := cmd.Targets[0]
			if g.In(q) {
				g.Block[g.ID(q)] = true
			}
		}
	}
	// Every nonmoving or acting role is included with a singleton goal: no collision with an idle teammate.
	if len(roles) > 0 {
		start := nav.State{}
		gs := make([][]int, len(roles))
		ids := make([]int, len(roles))
		locked := [3]bool{}
		moving := false
		for i, u := range roles {
			ids[i] = u.ID
			start[i] = g.ID(u.Pos)
			gs[i] = goals[u.ID]
			if len(gs[i]) == 0 {
				gs[i] = []int{start[i]}
			}
			if _, busy := out.Commands[strconv.Itoa(u.ID)]; busy {
				gs[i] = []int{start[i]}
				locked[i] = true
			}
			for _, cmd := range out.Commands {
				if cmd.Controller == strconv.Itoa(u.ID) {
					locked[i] = true
					gs[i] = []int{start[i]}
				}
			}
			if u.Type == "pioneer" && r.PhaseTask != "" && !m.Task.Abandon {
				locked[i] = true
				gs[i] = []int{start[i]}
			}
			for _, q := range gs[i] {
				if q != start[i] {
					moving = true
				}
			}
		}
		if moving {
			priority := taskPriority(r, m, roles, goals, out)
			taskRoute := priority > 0
			longest := -1
			for i, u := range roles {
				if d, ok := plan.ReturnRoles[u.ID]; ok && !locked[i] && d > longest {
					priority = i + 1
					longest = d
					taskRoute = false
				}
			}
			if priority == 0 {
				value := 0
				for i, u := range roles {
					if !locked[i] && len(u.Backpack) > value {
						value = len(u.Backpack)
						priority = i + 1
					}
				}
			}
			opt := nav.Options{MaxExpanded: e.Config.MaxExpanded, MaxStates: e.Config.MaxStates, Follow: e.Config.FollowMoves, Locked: locked, Priority: priority}
			if taskRoute {
				opt.MaxRounds = max(1, r.DayLeft()-e.Config.ReturnBuffer-1)
			}
			res := nav.Joint(ctx, g, start, gs, opt)
			tr.Paths = append(tr.Paths, PathTrace{Roles: ids, Goals: gs, Result: res})
			if len(res.Path) > 1 {
				for i, u := range roles {
					if res.Path[1][i] != start[i] {
						out.Commands[strconv.Itoa(u.ID)] = p.At("move", g.Pos(res.Path[1][i]))
					}
				}
			} else if !res.Optimal {
				tr.Notes = append(tr.Notes, "joint search budget exhausted: deterministic collision-free one-step fallback, NOT proven optimal")
				fallback(g, start, gs, roles, e.Config.FollowMoves, &out)
			}
			assessMovement(r, g, start, gs, roles, locked, e.Config.FollowMoves, m, &out, &tr, priority)
		}
	}
	newsTurn(r, e.Config, &m, &out)
	out = Validate(r, e.Config, out, &tr)
	// Round 1300 still receives its final actions; END follows their planning,
	// rather than discarding the last turn's defense and score opportunities.
	if r.Round == 1300 {
		transition(&plan, StateEnd, "final round dispatched")
	}
	if m.StrategyState != plan.State {
		tr.Notes = append(tr.Notes, "strategy transition: "+m.StrategyState+" -> "+plan.State)
	}
	m.StrategyState = plan.State
	for _, cmd := range out.Commands {
		if cmd.Action == "use" && isSummon(cmd.Name) {
			m.SummonsUsed++
		}
	}
	// Only accepted local actions affect predictive memory; judge snapshot remains authoritative.
	for _, w := range r.Weapons() {
		if cmd, ok := out.Commands[strconv.Itoa(w.ID)]; ok && cmd.Action == "attack" && w.Type == "rocket" {
			m.RocketNext[w.ID] = r.Round + 4
		}
	}
	m.LastPositions = map[int]p.Pos{}
	for _, u := range roles {
		m.LastPositions[u.ID] = u.Pos
	}
	m.Round = r.Round
	m.LastResponse = out
	return out, m, tr
}
func (t Trace) String() string {
	return fmt.Sprintf("mode=%s paths=%d rejected=%d", t.Mode, len(t.Paths), len(t.Rejected))
}
