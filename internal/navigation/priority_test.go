package navigation

import (
	"math/rand"
	"testing"
)

// Independent bounded layered enumeration, with no A* heuristic or heap.
func priorityOracle(g Grid, start State, goals [][]int, priority, horizon int, follow bool, locked [3]bool) (int, int, int) {
	n := len(goals)
	contains := func(i, v int) bool {
		for _, q := range goals[i] {
			if q == v {
				return true
			}
		}
		return false
	}
	type state struct {
		pos  State
		done bool
	}
	type cost struct{ arrival, moves int }
	layer := map[state]cost{{start, contains(priority, start[priority])}: {}}
	ba, bt, bm := -1, -1, -1
	for depth := 0; depth <= horizon; depth++ {
		nextLayer := map[state]cost{}
		for s, c := range layer {
			finished := true
			for i := 0; i < n; i++ {
				if !contains(i, s.pos[i]) {
					finished = false
				}
			}
			if finished && (ba < 0 || c.arrival < ba || c.arrival == ba && (depth < bt || depth == bt && c.moves < bm)) {
				ba, bt, bm = c.arrival, depth, c.moves
			}
			if depth == horizon {
				continue
			}
			next := s.pos
			var visit func(int)
			visit = func(i int) {
				if i < n {
					if locked[i] || i == priority && s.done {
						next[i] = s.pos[i]
						visit(i + 1)
						return
					}
					at := g.Pos(s.pos[i])
					for dy := -1; dy <= 1; dy++ {
						for dx := -1; dx <= 1; dx++ {
							p := Pos{X: at.X + dx, Y: at.Y + dy}
							if g.Free(p) {
								next[i] = g.ID(p)
								visit(i + 1)
							}
						}
					}
					return
				}
				for a := 0; a < n; a++ {
					for b := 0; b < a; b++ {
						if next[a] == next[b] || next[a] == s.pos[b] && next[b] == s.pos[a] || !follow && (next[a] == s.pos[b] || next[b] == s.pos[a]) {
							return
						}
					}
				}
				v := c
				if !s.done {
					v.arrival++
				}
				for a := 0; a < n; a++ {
					if next[a] != s.pos[a] {
						v.moves++
					}
				}
				k := state{next, s.done || contains(priority, next[priority])}
				old, ok := nextLayer[k]
				if !ok || v.arrival < old.arrival || v.arrival == old.arrival && v.moves < old.moves {
					nextLayer[k] = v
				}
			}
			visit(0)
		}
		layer = nextLayer
	}
	return ba, bt, bm
}

func TestPriorityAgainstExhaustiveOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(916))
	for test := 0; test < 40; test++ {
		g := New(3, 2)
		perm := rng.Perm(6)
		n := 2
		if test%5 == 0 {
			n = 3
		}
		start := State{}
		goals := make([][]int, n)
		locked := [3]bool{}
		for i := 0; i < n; i++ {
			start[i] = perm[i]
			goals[i] = []int{perm[n+i]}
		}
		if test%7 == 0 {
			locked[1] = true
			goals[1] = []int{start[1]}
		}
		if n == 2 && test%3 == 0 {
			g.Block[perm[5]] = true
		}
		if test%6 == 0 {
			goals[0] = append(goals[0], start[0])
		}
		follow := test%2 == 0
		priority := test % n
		horizon := 5
		a, rounds, moves := priorityOracle(g, start, goals, priority, horizon, follow, locked)
		got := Joint(t.Context(), g, start, goals, Options{Priority: priority + 1, MaxRounds: horizon, Follow: follow, Locked: locked, MaxExpanded: 100000, MaxStates: 100000})
		if !got.Optimal || got.Cost != rounds || rounds >= 0 && (got.PriorityArrival != a || got.Moves != moves) {
			t.Fatalf("case %d expected (%d,%d,%d), got %+v", test, a, rounds, moves, got)
		}
	}
}

func TestPriorityFixesDelayedTaskArrival(t *testing.T) {
	g := New(4, 3)
	start := State{0, 8}
	goals := [][]int{{1}, {11}}
	r := Joint(t.Context(), g, start, goals, Options{Priority: 1, MaxRounds: 6})
	if !r.Optimal || r.PriorityArrival != 1 || r.Cost != 3 || r.Path[1][0] != 1 {
		t.Fatal(r)
	}
	for _, s := range r.Path[1:] {
		if s[0] != 1 {
			t.Fatal("task role left its goal")
		}
	}
	limited := Joint(t.Context(), g, start, goals, Options{Priority: 1, MaxExpanded: 1})
	if limited.Optimal {
		t.Fatal("budget exhaustion claimed optimality")
	}
	deadline := Joint(t.Context(), g, start, goals, Options{Priority: 1, MaxRounds: 2})
	if deadline.Cost >= 0 || !deadline.Optimal {
		t.Fatal("impossible deadline accepted", deadline)
	}
}
