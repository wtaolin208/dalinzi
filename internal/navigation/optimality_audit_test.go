package navigation

import "testing"

// These cases document the difference between the planner's objective and
// game-level task latency. They intentionally do not change the production policy.
func TestAuditFollowingRestrictionCostsOneRound(t *testing.T) {
	g := New(3, 1)
	start := State{0, 1}
	goals := [][]int{{1}, {2}}
	conservative := Joint(t.Context(), g, start, goals, Options{Follow: false})
	following := Joint(t.Context(), g, start, goals, Options{Follow: true})
	if !conservative.Optimal || !following.Optimal || conservative.Cost != 2 || following.Cost != 1 {
		t.Fatalf("conservative=%+v following=%+v", conservative, following)
	}
	t.Logf("same corridor: following disabled=%d rounds, enabled=%d round", conservative.Cost, following.Cost)
}

func TestAuditMakespanDoesNotPrioritizeTaskRole(t *testing.T) {
	g := New(4, 3)
	start := State{0, 8}
	goals := [][]int{{1}, {11}}
	got := Joint(t.Context(), g, start, goals, Options{})
	if !got.Optimal || got.Cost != 3 {
		t.Fatalf("unexpected joint result: %+v", got)
	}
	firstArrival := -1
	for i, s := range got.Path {
		if s[0] == 1 {
			firstArrival = i
			break
		}
	}
	// A feasible competing plan completes the task role in one round and the
	// other role in three, preserving the optimal makespan of three.
	alternative := []State{start, {1, 9}, {1, 10}, {1, 11}}
	for i := 1; i < len(alternative); i++ {
		if !ValidTransition(alternative[i-1], alternative[i], 2, false) {
			t.Fatal("invalid alternative")
		}
		for role := 0; role < 2; role++ {
			a, b := g.Pos(alternative[i-1][role]), g.Pos(alternative[i][role])
			if a.X-b.X > 1 || b.X-a.X > 1 || a.Y-b.Y > 1 || b.Y-a.Y > 1 || !g.Free(b) {
				t.Fatal("nonlocal alternative")
			}
		}
	}
	if firstArrival <= 1 {
		t.Fatalf("tie-break changed; re-evaluate audit example: %v", got.Path)
	}
	t.Logf("optimal joint makespan=%d; task role arrives at %d, feasible alternative at 1; path=%v", got.Cost, firstArrival, got.Path)
}
