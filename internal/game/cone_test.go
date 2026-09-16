package game

import (
	p "competition/internal/protocol"
	"testing"
)

func TestGatlingConeBoundaries(t *testing.T) {
	origin := p.Pos{X: 3, Y: 3}
	for _, tc := range []struct {
		name       string
		directions []p.Pos
		want       bool
	}{
		{"same_direction", []p.Pos{{X: 1}, {X: 2}}, true},
		{"acute", []p.Pos{{X: 1}, {X: 1, Y: 1}}, true},
		{"exactly_90", []p.Pos{{X: 1}, {Y: 1}}, true},
		{"over_90", []p.Pos{{X: 1}, {X: -1, Y: 2}}, false},
		{"opposite", []p.Pos{{X: 1}, {X: -1}}, false},
		{"across_angle_wrap", []p.Pos{{X: 1, Y: -1}, {X: 1, Y: 1}}, true},
		{"three_within_90", []p.Pos{{X: 1}, {X: 1, Y: 1}, {Y: 1}}, true},
		// Both endpoints are within 90 degrees of the first vector, but not
		// of each other. Checking only against the first would be incorrect.
		{"must_check_every_pair", []p.Pos{{Y: 1}, {X: 1}, {X: -1}}, false},
		{"zero_vector", []p.Pos{{}, {X: 1}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			targets := make([]p.Pos, len(tc.directions))
			for i, d := range tc.directions {
				targets[i] = p.Pos{X: origin.X + d.X, Y: origin.Y + d.Y}
			}
			if got := cone(origin, targets); got != tc.want {
				t.Fatalf("cone=%v want %v", got, tc.want)
			}
		})
	}
}

func TestGatlingConeRejectsWholeAttack(t *testing.T) {
	r := small()
	r.Round = 71
	r.Our.Roles[0].Pos = p.Pos{X: 3, Y: 2}
	r.Our.Roles = append(r.Our.Roles, p.Role{ID: 3, Type: "gatling", Health: 1000, Level: 3, Pos: p.Pos{X: 3, Y: 3}})
	for _, valid := range []bool{true, false} {
		out := p.Empty()
		targets := []p.Pos{{X: 3, Y: 4}, {X: 4, Y: 3}, {X: 4, Y: 4}}
		if !valid {
			targets[2] = p.Pos{X: 2, Y: 3}
		}
		out.Commands["3"] = p.Command{Action: "attack", Controller: "1", Targets: targets}
		tr := Trace{}
		got := Validate(r, DefaultConfig(), out, &tr)
		if _, ok := got.Commands["3"]; ok != valid {
			t.Fatalf("valid=%v response=%+v trace=%+v", valid, got, tr)
		}
		if !valid && tr.Rejected["3"] != "gatling cone" {
			t.Fatalf("wrong rejection: %+v", tr)
		}
	}
}
