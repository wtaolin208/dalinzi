package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
	"context"
	"strings"
)

const (
	StateInit           = "INIT"
	StateDayAssess      = "DAY_ASSESS"
	StateDayBuild       = "DAY_BUILD"
	StateDayTask        = "DAY_TASK"
	StateDayTreasure    = "DAY_TREASURE"
	StateDayEconomy     = "DAY_ECONOMY"
	StateDayReturn      = "DAY_RETURN_HOME"
	StateNightAssess    = "NIGHT_ASSESS"
	StateNightEmergency = "NIGHT_EMERGENCY"
	StateNightDefend    = "NIGHT_DEFEND"
	StateNightOffense   = "NIGHT_OFFENSE"
	StateEnd            = "END"
)

type StateTransition struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

// The global mode controls which planners may run. Ordinary day modes permit
// complementary work by different roles; safety modes preempt that work.
type DispatchPolicy struct {
	Task      bool `json:"task"`
	Work      bool `json:"work"`
	Fire      bool `json:"fire"`
	Emergency bool `json:"emergency"`
	Return    bool `json:"return"`
	Summon    bool `json:"summon"`
}

func dispatchPolicy(b BattlePlan) DispatchPolicy {
	switch b.State {
	case StateDayBuild:
		if b.Emergency {
			return DispatchPolicy{Emergency: true, Return: true}
		}
		return DispatchPolicy{Task: true, Work: true, Summon: true}
	case StateDayTask, StateDayTreasure, StateDayEconomy:
		return DispatchPolicy{Task: true, Work: true, Summon: true}
	case StateDayReturn:
		return DispatchPolicy{Return: true}
	case StateNightEmergency:
		return DispatchPolicy{Emergency: true, Fire: true, Return: true}
	case StateNightDefend:
		return DispatchPolicy{Fire: true, Return: true}
	case StateNightAssess, StateNightOffense:
		// No verified night PvP action exists yet. Never substitute summon orders:
		// those are a daytime action even when a future night-offense module exists.
		return DispatchPolicy{Return: true}
	}
	return DispatchPolicy{}
}

func explicitlyDestroyed(team p.Team) bool {
	found := false
	for _, u := range team.Roles {
		if u.Type == "station" {
			found = true
			if u.Health > 0 {
				return false
			}
		}
	}
	return found // Absence in a partial snapshot does not prove destruction.
}
func terminalBeforeTurn(r p.Request, m Memory) bool {
	return r.Round > 1300 || (m.StrategyState == StateEnd && r.Round >= m.Round) || explicitlyDestroyed(r.Our) && explicitlyDestroyed(r.Enemy)
}
func transition(b *BattlePlan, to, reason string) {
	b.Transitions = append(b.Transitions, StateTransition{b.State, to, reason})
	b.State = to
	b.Reason = reason
}

func assessFSM(ctx context.Context, r p.Request, g nav.Grid, c Config, m Memory, pairs []Pair) BattlePlan {
	b := strategyPlan(r, g, c, pairs)
	selected := b.State
	b.State = m.StrategyState
	if b.State == "" {
		transition(&b, StateInit, "first snapshot or match reset")
	}
	if terminalBeforeTurn(r, m) {
		transition(&b, StateEnd, "confirmed terminal snapshot")
		b.Policy = dispatchPolicy(b)
		return b
	}
	assess := StateDayAssess
	if !r.Daylight() {
		assess = StateNightAssess
	}
	transition(&b, assess, "reassess snapshot, feedback, routes and threats")
	switch {
	case b.Emergency:
		selected = StateNightEmergency
		if r.Daylight() {
			selected = StateDayBuild
		}
	case !r.Daylight():
		selected = StateNightAssess
		for _, bot := range r.Robots.Roles {
			if bot.Health > 0 && ownThreat(r, bot.Target) {
				selected = StateNightDefend
				break
			}
		}
	case len(b.ReturnRoles) > 0 || m.Task.Abandon || safetyInterrupt(r, m) != "":
		selected = StateDayReturn
	default:
		selected = dayIntent(ctx, r, g, c, m)
	}
	if selected != b.State {
		transition(&b, selected, "highest priority feasible intent")
	}
	if reason := safetyInterrupt(r, m); reason != "" && r.Daylight() && !b.Emergency {
		b.Reason = reason
		if len(b.Transitions) > 0 {
			b.Transitions[len(b.Transitions)-1].Reason = reason
		}
	}
	b.Policy = dispatchPolicy(b)
	return b
}

func safetyInterrupt(r p.Request, m Memory) string {
	if m.ActionErrorStreak >= 3 {
		return "three consecutive snapshots with action errors"
	}
	alive := map[int]bool{}
	for _, u := range r.Mobiles() {
		alive[u.ID] = true
		if m.Stuck[u.ID] >= 3 {
			return "repeated movement failure; return and replan"
		}
	}
	if m.Round == r.Round-1 {
		for id := range m.LastPositions {
			if !alive[id] {
				return "mobile role lost; rebuild defensive assignment"
			}
		}
	}
	return ""
}

func dayIntent(ctx context.Context, r p.Request, g nav.Grid, c Config, m Memory) string {
	// Assess real candidates, including approach paths, rather than inferring
	// intent from the eventual move command, which has no semantic target type.
	build := false
	for _, u := range r.Mobiles() {
		if u.Type != "worker" {
			continue
		}
		choices := workerOptions(ctx, r, u, g, c, r.Our.Gold, m.Work[u.ID], m)
		if len(choices) > 0 {
			cmd := choices[0].command
			build = build || cmd.Action == "build" || strings.Contains(cmd.Name, "UpgradeVoucher") || cmd.Name == "WallFixer"
		}
	}
	if build {
		return StateDayBuild
	}
	if r.PhaseTask != "" && !m.Task.Abandon {
		return StateDayTask
	}
	if c.EnableTasks {
		for _, u := range r.Mobiles() {
			if u.Type != "pioneer" {
				continue
			}
			if chooseTask(r, u, g, c, m) != nil {
				return StateDayTask
			}
			copy := CloneMemory(m)
			out := p.Empty()
			goals := map[int][]int{}
			gold := max(0, r.Our.Gold-constructionReserve(r, c))
			if planTreasure(r, u, g, c, &copy, &out, goals, &gold) {
				return StateDayTreasure
			}
		}
	}
	return StateDayEconomy
}
