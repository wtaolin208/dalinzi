package game

import (
	p "competition/internal/protocol"
	"testing"
)

func TestReadiness(t *testing.T) {
	r := small()
	c := DefaultConfig()
	if len(c.Readiness(r)) == 0 {
		t.Fatal("missing profile accepted")
	}
	c.Profiles[r.Our.Type] = Profile{Verified: true, Weapons: []Site{{Pos: p.Pos{X: 5, Y: 5}, Kind: "rocket"}}}
	if issues := c.Readiness(r); len(issues) > 0 {
		t.Fatal(issues)
	}
	f := c.Profiles[r.Our.Type]
	f.Weapons[0].Pos.X = 100
	c.Profiles[r.Our.Type] = f
	if len(c.Readiness(r)) == 0 {
		t.Fatal("out of map site accepted")
	}
	f.Weapons[0].Pos.X = -1
	c.Profiles[r.Our.Type] = f
	if c.Validate() == nil {
		t.Fatal("negative coordinate accepted")
	}
}
