package navigation

import (
	"context"
	"math/rand"
	"testing"
)

func TestDiagonalAndGoalSet(t *testing.T) {
	g := New(3, 3)
	g.Block[1] = true
	g.Block[3] = true
	path := g.Shortest(0, []int{8, 6})
	if len(path) != 3 {
		t.Fatalf("diagonal corners must be passable: %v", path)
	}
	if got := g.Shortest(0, []int{0}); len(got) != 1 {
		t.Fatal(got)
	}
	g.Block[4] = true
	if got := g.Shortest(0, []int{8}); got != nil {
		t.Fatal(got)
	}
}

// Independent exhaustive joint BFS oracle, without A* distances/pruning.
func oracle(g Grid, start State, goals [][]int, follow bool) int {
	n := len(goals)
	goal := func(s State) bool {
		for i := 0; i < n; i++ {
			ok := false
			for _, q := range goals[i] {
				if q == s[i] {
					ok = true
				}
			}
			if !ok {
				return false
			}
		}
		return true
	}
	q := []State{start}
	d := map[State]int{start: 0}
	for head := 0; head < len(q); head++ {
		s := q[head]
		if goal(s) {
			return d[s]
		}
		next := s
		var walk func(int)
		walk = func(i int) {
			if i < n {
				p := g.Pos(s[i])
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						v := Pos{X: p.X + dx, Y: p.Y + dy}
						if g.Free(v) {
							next[i] = g.ID(v)
							walk(i + 1)
						}
					}
				}
				return
			}
			for a := 0; a < n; a++ {
				for b := 0; b < a; b++ {
					if next[a] == next[b] || next[a] == s[b] && next[b] == s[a] || !follow && (next[a] == s[b] || next[b] == s[a]) {
						return
					}
				}
			}
			if _, seen := d[next]; !seen {
				d[next] = d[s] + 1
				q = append(q, next)
			}
		}
		walk(0)
	}
	return -1
}
func TestJointAgainstOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(7021))
	for test := 0; test < 35; test++ {
		g := New(3, 3)
		perm := rng.Perm(9)
		n := 2
		if test%5 == 0 {
			n = 3
		}
		s := State{}
		goals := make([][]int, n)
		for i := 0; i < n; i++ {
			s[i] = perm[i]
			goals[i] = []int{perm[n+i]}
		}
		if test%3 == 0 {
			g.Block[perm[8]] = true
		}
		follow := test%2 == 0
		want := oracle(g, s, goals, follow)
		got := Joint(context.Background(), g, s, goals, Options{MaxExpanded: 100000, MaxStates: 100000, Follow: follow})
		if !got.Optimal || got.Cost != want {
			t.Fatalf("case %d want %d got %+v", test, want, got)
		}
		for i := 1; i < len(got.Path); i++ {
			if !ValidTransition(got.Path[i-1], got.Path[i], n, follow) {
				t.Fatal("invalid path")
			}
		}
	}
}
func TestJointBudgetAndLocked(t *testing.T) {
	g := New(5, 1)
	s := State{0, 2}
	goals := [][]int{{4}, {2}}
	a := Joint(context.Background(), g, s, goals, Options{Locked: [3]bool{false, true}})
	if a.Reason != "infeasible" || !a.Optimal {
		t.Fatal(a)
	}
	b := Joint(context.Background(), New(8, 8), State{0}, [][]int{{63}}, Options{MaxExpanded: 1})
	if b.Optimal || b.Reason != "expansion_limit" {
		t.Fatal(b)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	v := Joint(ctx, g, State{0}, [][]int{{4}}, Options{})
	if v.Optimal || v.Reason != "deadline" {
		t.Fatal(v)
	}
}
func BenchmarkJointOpen(b *testing.B) {
	g := New(41, 32)
	for i := 0; i < b.N; i++ {
		r := Joint(context.Background(), g, State{0, 41, 82}, [][]int{{1250}, {1251}, {1252}}, Options{MaxExpanded: 12000, MaxStates: 60000})
		if r.Cost < 0 {
			b.Fatal(r.Reason)
		}
	}
}
