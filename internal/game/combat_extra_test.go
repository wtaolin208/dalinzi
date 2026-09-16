package game

import (
	p "competition/internal/protocol"
	"reflect"
	"testing"
)

func TestShotDamageBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   string
		robots []p.Robot
		want   []int
	}{
		{"gatling_first_hit", "gatling", []p.Robot{{ID: 2, Pos: p.Pos{X: 3, Y: 1}, Health: 40}, {ID: 1, Pos: p.Pos{X: 2, Y: 1}, Health: 5}}, []int{0, 10}},
		{"dead_robot_does_not_block", "gatling", []p.Robot{{ID: 1, Pos: p.Pos{X: 2, Y: 1}, Health: 0}, {ID: 2, Pos: p.Pos{X: 3, Y: 1}, Health: 40}}, []int{0, 10}},
		{"railgun_energy_exhausted", "railgun", []p.Robot{{ID: 1, Pos: p.Pos{X: 2, Y: 1}, Health: 40}, {ID: 2, Pos: p.Pos{X: 3, Y: 1}, Health: 40}}, []int{30, 0}},
		{"rocket_center_edge_outside", "rocket", []p.Robot{{ID: 1, Pos: p.Pos{X: 4, Y: 1}, Health: 40}, {ID: 2, Pos: p.Pos{X: 5, Y: 2}, Health: 40}, {ID: 3, Pos: p.Pos{X: 6, Y: 1}, Health: 40}}, []int{20, 10, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := small()
			r.Robots.Roles = tc.robots
			got := shotDamage(r, p.Role{Type: tc.kind, Level: 3, Pos: p.Pos{X: 1, Y: 1}}, []p.Pos{{X: 4, Y: 1}})
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("damage=%v, want %v", got, tc.want)
			}
		})
	}
}

func BenchmarkCombatCandidates(b *testing.B) {
	for _, kind := range []string{"gatling", "railgun", "rocket"} {
		b.Run(kind, func(b *testing.B) {
			r := small()
			r.Map = p.MapInfo{Width: 41, Height: 32}
			for i := 0; i < 64; i++ {
				r.Robots.Roles = append(r.Robots.Roles, p.Robot{ID: i + 100, Type: "middleRobot", Health: 40, Pos: p.Pos{X: 10 + i%8, Y: 10 + i/8}})
			}
			w := p.Role{Type: kind, Level: 3, Pos: p.Pos{X: 9, Y: 9}}
			c := DefaultConfig()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if got := candidates(r, w, c); len(got) == 0 {
					b.Fatal("no attack candidates")
				}
			}
		})
	}
}
