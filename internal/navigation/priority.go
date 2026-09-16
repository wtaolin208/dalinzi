package navigation

import (
	"container/heap"
	"context"
)

type lexCost struct{ arrival, rounds, moves int }

func lessCost(a, b lexCost) bool {
	if a.arrival != b.arrival {
		return a.arrival < b.arrival
	}
	if a.rounds != b.rounds {
		return a.rounds < b.rounds
	}
	return a.moves < b.moves
}

type priorityKey struct {
	position State
	arrived  bool
	time     int
}
type priorityNode struct {
	key         priorityKey
	cost, bound lexCost
	parent      *priorityNode
	seq         int
}
type priorityQueue []*priorityNode

func (q priorityQueue) Len() int { return len(q) }
func (q priorityQueue) Less(i, j int) bool {
	if q[i].bound != q[j].bound {
		return lessCost(q[i].bound, q[j].bound)
	}
	return q[i].seq < q[j].seq
}
func (q priorityQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *priorityQueue) Push(v any)   { *q = append(*q, v.(*priorityNode)) }
func (q *priorityQueue) Pop() any     { a := *q; v := a[len(a)-1]; *q = a[:len(a)-1]; return v }

// Lexicographic A*: first priority arrival, then all-arrived time, then moves.
// Once the priority role arrives it stays to perform its task. With a deadline,
// elapsed time is part of the key so an earlier-arrival but slower overall path
// cannot incorrectly dominate the only deadline-feasible path.
func priorityJoint(ctx context.Context, g Grid, start State, goals [][]int, opt Options) Result {
	n := len(goals)
	p := opt.Priority - 1
	r := Result{Cost: -1, Lower: -1, Agents: n, Objective: "priority_arrival_then_makespan_then_moves"}
	if n < 1 || n > 3 || p < 0 || p >= n {
		r.Reason = "invalid_priority"
		return r
	}
	if opt.MaxExpanded <= 0 {
		opt.MaxExpanded = 20000
	}
	if opt.MaxStates <= 0 {
		opt.MaxStates = 100000
	}
	ds := make([][]int, n)
	for i := 0; i < n; i++ {
		if start[i] < 0 || start[i] >= len(g.Block) || g.Block[start[i]] {
			r.Reason = "invalid_start"
			return r
		}
		for j := 0; j < i; j++ {
			if start[i] == start[j] {
				r.Reason = "overlapping_start"
				return r
			}
		}
		ds[i] = g.Distances(goals[i])
		if ds[i][start[i]] < 0 {
			r.Reason = "infeasible"
			r.Optimal = true
			return r
		}
	}
	bound := func(s State, done bool, c lexCost) lexCost {
		b := c
		longest, total := 0, 0
		for i := 0; i < n; i++ {
			longest = max(longest, ds[i][s[i]])
			total += ds[i][s[i]]
		}
		if !done {
			b.arrival += ds[p][s[p]]
		}
		b.rounds += longest
		b.moves += total
		return b
	}
	key := priorityKey{position: start, arrived: ds[p][start[p]] == 0}
	root := &priorityNode{key: key}
	root.bound = bound(start, key.arrived, root.cost)
	q := priorityQueue{root}
	heap.Init(&q)
	best := map[priorityKey]lexCost{key: {}}
	seq := 0
	for q.Len() > 0 {
		cur := heap.Pop(&q).(*priorityNode)
		if best[cur.key] != cur.cost {
			continue
		}
		if err := ctx.Err(); err != nil {
			r.Reason = "deadline"
			return r
		}
		if cur.bound.rounds == cur.cost.rounds {
			r.Cost = cur.cost.rounds
			r.PriorityArrival = cur.cost.arrival
			r.Moves = cur.cost.moves
			r.Optimal = true
			r.Reason = "optimal"
			for at := cur; at != nil; at = at.parent {
				r.Path = append(r.Path, at.key.position)
			}
			for i, j := 0, len(r.Path)-1; i < j; i, j = i+1, j-1 {
				r.Path[i], r.Path[j] = r.Path[j], r.Path[i]
			}
			return r
		}
		if r.Expanded >= opt.MaxExpanded {
			r.Reason = "expansion_limit"
			return r
		}
		r.Expanded++
		moves := make([][]int, n)
		for i := 0; i < n; i++ {
			if opt.Locked[i] || i == p && cur.key.arrived {
				moves[i] = []int{cur.key.position[i]}
			} else {
				moves[i] = g.Neighbors(cur.key.position[i], true)
			}
		}
		next := cur.key.position
		limit := false
		var visit func(int)
		visit = func(i int) {
			if limit {
				return
			}
			if i < n {
				for _, v := range moves[i] {
					next[i] = v
					visit(i + 1)
				}
				return
			}
			if next == cur.key.position || !ValidTransition(cur.key.position, next, n, opt.Follow) {
				return
			}
			c := cur.cost
			c.rounds++
			if !cur.key.arrived {
				c.arrival++
			}
			for j := 0; j < n; j++ {
				if ds[j][next[j]] < 0 {
					return
				}
				if next[j] != cur.key.position[j] {
					c.moves++
				}
			}
			k := priorityKey{position: next, arrived: cur.key.arrived || ds[p][next[p]] == 0}
			b := bound(next, k.arrived, c)
			if opt.MaxRounds > 0 {
				if b.rounds > opt.MaxRounds {
					return
				}
				k.time = c.rounds
			}
			if old, ok := best[k]; ok && !lessCost(c, old) {
				return
			}
			if _, ok := best[k]; !ok && len(best) >= opt.MaxStates {
				limit = true
				return
			}
			best[k] = c
			seq++
			heap.Push(&q, &priorityNode{key: k, cost: c, bound: b, parent: cur, seq: seq})
		}
		visit(0)
		if limit {
			r.Reason = "state_limit"
			return r
		}
	}
	r.Optimal = true
	r.Reason = "infeasible"
	return r
}
