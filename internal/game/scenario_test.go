package game

import (
	p "competition/internal/protocol"
	"context"
	"testing"
)

func TestEconomicActions(t *testing.T) {
	for _, tc := range []struct {
		name, action, item string
		setup              func(*p.Request, *Config)
	}{
		{"mine", "collect", "", func(r *p.Request, c *Config) { r.Map.Zones = []p.Zone{{Pos: p.Pos{X: 2, Y: 1}, Type: "copper"}} }},
		{"sell", "sell", "copper", func(r *p.Request, c *Config) {
			r.Map.Zones = []p.Zone{{Pos: p.Pos{X: 2, Y: 1}, Type: "vendor"}}
			r.Vendor = []p.ShopItem{{Name: "copper", Price: 5}}
			r.Our.Roles[0].Backpack = []string{"copper", "copper"}
		}},
		{"build_weapon", "build", "rocket", func(r *p.Request, c *Config) {
			c.Profiles[r.Our.Type] = Profile{Verified: true, Weapons: []Site{{Pos: p.Pos{X: 2, Y: 1}, Kind: "rocket"}}}
		}},
		{"build_wall", "build", "wall", func(r *p.Request, c *Config) {
			r.Our.Roles[0].Backpack = []string{"stone"}
			c.Profiles[r.Our.Type] = Profile{Verified: true, Walls: []p.Pos{{X: 2, Y: 1}}}
		}},
		{"buy_voucher", "buy", "StationUpgradeVoucher1", func(r *p.Request, c *Config) {
			r.Our.Gold = 200
			r.Our.Roles = append(r.Our.Roles, p.Role{ID: 4, Type: "station", Health: 1000, Level: 1, Pos: p.Pos{X: 5, Y: 5}})
			r.Map.Zones = []p.Zone{{Pos: p.Pos{X: 2, Y: 1}, Type: "weaponShop"}}
			r.Shop = []p.ShopItem{{Name: "StationUpgradeVoucher1", Price: 50}}
		}},
		{"heal", "use", "Medicine", func(r *p.Request, c *Config) {
			r.Our.Roles[0].Health = 50
			r.Our.Roles[0].Backpack = []string{"Medicine"}
		}},
		{"upgrade_base", "use", "StationUpgradeVoucher1", func(r *p.Request, c *Config) {
			r.Our.Roles[0].Backpack = []string{"StationUpgradeVoucher1"}
			r.Our.Roles = append(r.Our.Roles, p.Role{ID: 4, Type: "station", Health: 100, Level: 1, Pos: p.Pos{X: 2, Y: 2}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := small()
			r.Our.Roles = r.Our.Roles[:1]
			c := DefaultConfig()
			tc.setup(&r, &c)
			out, _, tr := (Engine{c}).Decide(context.Background(), r, Memory{})
			cmd := out.Commands["1"]
			if cmd.Action != tc.action || cmd.Name != tc.item {
				t.Fatalf("got %+v, trace %+v", out, tr)
			}
			if len(tr.Rejected) > 0 {
				t.Fatalf("planner generated rejected actions: %v", tr.Rejected)
			}
		})
	}
}

func TestPioneerTaskAndTreasure(t *testing.T) {
	for _, treasure := range []bool{false, true} {
		r := small()
		r.Our.Roles = r.Our.Roles[:1]
		r.Our.Roles[0].Type = "pioneer"
		m := Memory{Team: r.Our.ID, Side: r.Our.Type}
		want := "acceptTask"
		if treasure {
			m.Treasure = &Treasure{Target: p.Pos{X: 2, Y: 1}, Earliest: 1, Latest: 20, Items: []string{"A"}}
			r.Our.Roles[0].Backpack = []string{"A"}
			want = "summonTreasure"
		} else {
			r.Our.Tasks = []p.PlayerTask{{Pos: p.Pos{X: 2, Y: 1}, Valid: true, Score: 20, Gold: 10}}
			r.Map.Zones = []p.Zone{{Pos: p.Pos{X: 2, Y: 1}, Type: "challengerTaskPoint1"}}
		}
		out, after, tr := (Engine{DefaultConfig()}).Decide(context.Background(), r, m)
		if out.Commands["1"].Action != want || len(tr.Rejected) > 0 {
			t.Fatalf("%s: %+v %+v", want, out, tr)
		}
		if treasure && after.LastTreasureRound != 1 {
			t.Fatal("missing treasure feedback association")
		}
		if !treasure && after.Task.Start != 1 {
			t.Fatal("missing task start")
		}
	}
}

func TestNightCombatAndCooldown(t *testing.T) {
	r := small()
	r.Round = 71
	r.Our.Roles = r.Our.Roles[:1]
	zero := 0
	r.Our.Roles = append(r.Our.Roles, p.Role{ID: 3, Type: "rocket", Health: 1000, Level: 1, Pos: p.Pos{X: 2, Y: 1}, Cooldown: &zero})
	r.Robots.Roles = []p.Robot{{ID: 100, Type: "middleRobot", Health: 40, Pos: p.Pos{X: 4, Y: 1}, Target: r.Our.Type}}
	out, m, tr := (Engine{DefaultConfig()}).Decide(context.Background(), r, Memory{})
	if out.Commands["3"].Action != "attack" || m.RocketNext[3] != 75 || len(tr.Rejected) > 0 {
		t.Fatalf("%+v %+v", out, tr)
	}
	for _, cooldown := range []*int{nil, new(int)} {
		r.Round = 72
		r.Our.Roles[1].Cooldown = cooldown
		if cooldown != nil {
			*cooldown = 3
		}
		out, _, _ := (Engine{DefaultConfig()}).Decide(context.Background(), r, m)
		if out.Commands["3"].Action == "attack" {
			t.Fatal("shot during cooldown")
		}
	}
}

func TestNoncanonicalControllerRejected(t *testing.T) {
	r := small()
	r.Round = 71
	r.Our.Roles = append(r.Our.Roles, p.Role{ID: 3, Type: "gatling", Health: 1000, Level: 1, Pos: p.Pos{X: 1, Y: 2}})
	out := p.Empty()
	out.Commands["1"] = p.At("move", p.Pos{X: 2, Y: 1})
	out.Commands["3"] = p.Command{Action: "attack", Controller: "01", Targets: []p.Pos{{X: 2, Y: 3}}}
	tr := Trace{}
	got := Validate(r, DefaultConfig(), out, &tr)
	if _, ok := got.Commands["3"]; ok {
		t.Fatal("alternate controller spelling bypassed actor exclusivity")
	}
}

func TestConstructionPreservesRoute(t *testing.T) {
	r := small()
	r.Map = p.MapInfo{Width: 6, Height: 1, Zones: []p.Zone{{Pos: p.Pos{X: 5}, Type: "vendor"}}}
	r.Our.Roles = []p.Role{{ID: 1, Type: "worker", Health: 220, Pos: p.Pos{X: 1}}}
	c := DefaultConfig()
	c.Profiles[r.Our.Type] = Profile{Verified: true, Weapons: []Site{{Pos: p.Pos{X: 2}, Kind: "rocket"}}}
	out := p.Empty()
	out.Commands["1"] = p.Command{Action: "build", Name: "rocket", Targets: []p.Pos{{X: 2}}}
	tr := Trace{}
	got := Validate(r, c, out, &tr)
	if len(got.Commands) > 0 {
		t.Fatal("construction cuts off vendor")
	}
}

func TestRecursiveMoveConflict(t *testing.T) {
	r := small()
	r.Our.Roles = []p.Role{{ID: 1, Type: "worker", Health: 1, Pos: p.Pos{X: 1, Y: 1}}, {ID: 2, Type: "worker", Health: 1, Pos: p.Pos{X: 2, Y: 1}}, {ID: 3, Type: "pioneer", Health: 1, Pos: p.Pos{X: 3, Y: 1}}}
	c := DefaultConfig()
	c.FollowMoves = true
	out := p.Empty()
	out.Commands["1"] = p.At("move", p.Pos{X: 2, Y: 1})
	out.Commands["2"] = p.At("move", p.Pos{X: 3, Y: 1})
	tr := Trace{}
	got := Validate(r, c, out, &tr)
	if len(got.Commands) > 0 {
		t.Fatal("rejected leader must stop follower")
	}
	out.Commands["3"] = p.At("move", p.Pos{X: 4, Y: 1})
	tr = Trace{}
	got = Validate(r, c, out, &tr)
	if len(got.Commands) != 3 {
		t.Fatal("valid following chain rejected")
	}
}

func TestTaskToolFailureAndTimeout(t *testing.T) {
	r := small()
	r.Round = 10
	r.PhaseTask = "task"
	r.Our.Roles[0].Type = "pioneer"
	m := Memory{Task: TaskMemory{Text: "task", Session: "s", Start: 1, Timeout: 10, Answer: "saved", Pending: Pending{Kind: "command", Round: 9, Session: "s"}, Recipe: "failed"}}
	r.CmdResult = "[TIMEOUT]\npartial"
	out := p.Empty()
	tr := Trace{}
	taskTurn(r, r.Our.Roles[0], DefaultConfig(), &m, &out, &tr)
	if out.Commands["1"].Answer == nil || *out.Commands["1"].Answer != "saved" || out.Execute != "" {
		t.Fatalf("timeout fallback %+v", out)
	}
}
