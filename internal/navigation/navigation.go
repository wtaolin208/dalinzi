// Package navigation supplies exact static shortest paths and bounded joint A*.
// Optimal means optimal in the supplied graph, not against unseen enemy moves.
package navigation

import (
	"competition/internal/protocol"
	"container/heap"
	"context"
)

type Pos = protocol.Pos
type Grid struct {
	W, H  int
	Block []bool
}

func New(w, h int) Grid        { return Grid{w, h, make([]bool, w*h)} }
func (g Grid) In(p Pos) bool   { return p.X >= 0 && p.Y >= 0 && p.X < g.W && p.Y < g.H }
func (g Grid) ID(p Pos) int    { return p.Y*g.W + p.X }
func (g Grid) Pos(i int) Pos   { return Pos{X: i % g.W, Y: i / g.W} }
func (g Grid) Free(p Pos) bool { return g.In(p) && !g.Block[g.ID(p)] }
func (g Grid) Clone() Grid     { return Grid{g.W, g.H, append([]bool(nil), g.Block...)} }
func (g Grid) Neighbors(id int, wait bool) []int {
	p := g.Pos(id)
	a := make([]int, 0, 9)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 && !wait {
				continue
			}
			q := Pos{X: p.X + dx, Y: p.Y + dy}
			if g.Free(q) {
				a = append(a, g.ID(q))
			}
		}
	}
	return a
}
func (g Grid) Around(cells []Pos) []int {
	seen := make([]bool, len(g.Block))
	var a []int
	for _, p := range cells {
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				q := Pos{X: p.X + dx, Y: p.Y + dy}
				if g.Free(q) && !seen[g.ID(q)] {
					seen[g.ID(q)] = true
					a = append(a, g.ID(q))
				}
			}
		}
	}
	return a
}
func (g Grid) Distances(goals []int) []int {
	d := make([]int, len(g.Block))
	for i := range d {
		d[i] = -1
	}
	q := make([]int, 0, len(d))
	for _, i := range goals {
		if i >= 0 && i < len(d) && !g.Block[i] && d[i] < 0 {
			d[i] = 0
			q = append(q, i)
		}
	}
	for head := 0; head < len(q); head++ {
		for _, v := range g.Neighbors(q[head], false) {
			if d[v] < 0 {
				d[v] = d[q[head]] + 1
				q = append(q, v)
			}
		}
	}
	return d
}
func (g Grid) Shortest(start int, goals []int) []int {
	if start < 0 || start >= len(g.Block) || g.Block[start] {
		return nil
	}
	d := g.Distances(goals)
	if d[start] < 0 {
		return nil
	}
	path := []int{start}
	for d[start] > 0 {
		for _, n := range g.Neighbors(start, false) {
			if d[n] == d[start]-1 {
				start = n
				path = append(path, n)
				break
			}
		}
	}
	return path
}

type State [3]int
type Options struct {
	MaxExpanded int
	MaxStates   int
	Follow      bool
	Locked      [3]bool
	// Priority is one-based; zero preserves the makespan objective.
	Priority  int
	MaxRounds int
}
type Result struct {
	Path            []State `json:"path,omitempty"`
	Cost            int     `json:"cost"`
	Lower           int     `json:"lowerBound"`
	Optimal         bool    `json:"optimal"`
	Expanded        int     `json:"expanded"`
	Reason          string  `json:"reason"`
	Agents          int     `json:"agents"`
	Objective       string  `json:"objective,omitempty"`
	PriorityArrival int     `json:"priorityArrival,omitempty"`
	Moves           int     `json:"moves,omitempty"`
}
type node struct {
	s         State
	g, f, seq int
}
type queue []node

func (q queue) Len() int { return len(q) }
func (q queue) Less(i, j int) bool {
	if q[i].f != q[j].f {
		return q[i].f < q[j].f
	}
	if q[i].g != q[j].g {
		return q[i].g > q[j].g
	}
	return q[i].seq < q[j].seq
}
func (q queue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *queue) Push(x any)   { *q = append(*q, x.(node)) }
func (q *queue) Pop() any     { a := *q; n := a[len(a)-1]; *q = a[:len(a)-1]; return n }
func ValidTransition(old, next State, n int, follow bool) bool {
	for i := 0; i < n; i++ {
		for j := 0; j < i; j++ {
			if next[i] == next[j] || next[i] == old[j] && next[j] == old[i] {
				return false
			}
			if !follow && (next[i] == old[j] || next[j] == old[i]) {
				return false
			}
		}
	}
	return true
}
func Joint(ctx context.Context, g Grid, start State, goals [][]int, opt Options) Result {
	if opt.Priority > 0 {
		return priorityJoint(ctx, g, start, goals, opt)
	}
	n := len(goals)
	r := Result{Cost: -1, Lower: -1, Agents: n}
	if n < 1 || n > 3 {
		r.Reason = "invalid_agent_count"
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
	h := func(s State) int {
		v := 0
		for i := 0; i < n; i++ {
			if ds[i][s[i]] < 0 {
				return -1
			}
			v = max(v, ds[i][s[i]])
		}
		return v
	}
	r.Lower = h(start)
	q := queue{{start, 0, r.Lower, 0}}
	heap.Init(&q)
	best := map[State]int{start: 0}
	parent := map[State]State{}
	seq := 0
	for q.Len() > 0 {
		cur := heap.Pop(&q).(node)
		if best[cur.s] != cur.g {
			continue
		}
		r.Lower = cur.f
		if h(cur.s) == 0 {
			r.Cost = cur.g
			r.Lower = cur.g
			r.Optimal = true
			r.Reason = "optimal"
			for s := cur.s; ; s = parent[s] {
				r.Path = append(r.Path, s)
				if s == start {
					break
				}
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
		if e := ctx.Err(); e != nil {
			r.Reason = "deadline"
			return r
		}
		r.Expanded++
		moves := make([][]int, n)
		for i := 0; i < n; i++ {
			if opt.Locked[i] {
				moves[i] = []int{cur.s[i]}
			} else {
				moves[i] = g.Neighbors(cur.s[i], true)
			}
		}
		next := cur.s
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
			if next == cur.s || !ValidTransition(cur.s, next, n, opt.Follow) {
				return
			}
			hv := h(next)
			if hv < 0 {
				return
			}
			ng := cur.g + 1
			if old, ok := best[next]; ok && old <= ng {
				return
			}
			if _, ok := best[next]; !ok && len(best) >= opt.MaxStates {
				limit = true
				return
			}
			best[next] = ng
			parent[next] = cur.s
			seq++
			heap.Push(&q, node{next, ng, ng + hv, seq})
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
