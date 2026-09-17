package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

type Site struct {
	Pos  p.Pos  `json:"pos"`
	Kind string `json:"kind"`
}
type Profile struct {
	Verified bool    `json:"verified"`
	Weapons  []Site  `json:"weapons"`
	Walls    []p.Pos `json:"walls"`
}
type Config struct {
	ReturnAssignments      map[int]p.Role     `json:"-"`
	Strategy               StrategyConfig     `json:"strategy"`
	Recipes                []Recipe           `json:"recipes,omitempty"`
	Profiles               map[string]Profile `json:"profiles"`
	ExperimentalDemoLayout bool               `json:"experimentalDemoLayout"`
	FollowMoves            bool               `json:"followMoves"`
	MaxExpanded            int                `json:"maxExpanded"`
	MaxStates              int                `json:"maxStates"`
	ReturnBuffer           int                `json:"returnBuffer"`
	MineBatch              int                `json:"mineBatch"`
	ReserveGold            int                `json:"reserveGold"`
	CombatCandidates       int                `json:"combatCandidates"`
	CombatBeam             int                `json:"combatBeam"`
	AllowDuplicateShots    bool               `json:"allowDuplicateShots"`
	EnableTasks            bool               `json:"enableTasks"`
	EnableNews             bool               `json:"enableNews"`
	EnableSandboxCommands  bool               `json:"enableSandboxCommands"`
	MaxToolBytes           int                `json:"maxToolBytes"`
}

func DefaultConfig() Config {
	return Config{Strategy: defaultStrategy(), Profiles: map[string]Profile{}, MaxExpanded: 12000, MaxStates: 60000, ReturnBuffer: 5, MineBatch: 12, ReserveGold: 25, CombatCandidates: 12, CombatBeam: 48, EnableTasks: true, EnableNews: true, EnableSandboxCommands: true, MaxToolBytes: 24000}
}
func LoadConfig(path string) (Config, error) {
	c := DefaultConfig()
	if path != "" {
		b, e := os.ReadFile(path)
		if e != nil {
			return c, e
		}
		if e = json.Unmarshal(b, &c); e != nil {
			return c, e
		}
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	if err := c.Strategy.Validate(); err != nil {
		return err
	}
	for _, recipe := range c.Recipes {
		if recipe.Name == "" || recipe.Pattern == "" || recipe.Python == "" {
			return fmt.Errorf("empty task recipe")
		}
		if _, err := regexp.Compile(recipe.Pattern); err != nil {
			return fmt.Errorf("recipe %s: %w", recipe.Name, err)
		}
	}
	if c.MaxExpanded < 1 || c.MaxStates < 1 || c.ReturnBuffer < 0 || c.MineBatch < 1 || c.ReserveGold < 0 || c.CombatCandidates < 1 || c.CombatCandidates > 64 || c.CombatBeam < 1 || c.CombatBeam > 512 || c.MaxToolBytes < 1 || c.MaxToolBytes > 1<<20 {
		return fmt.Errorf("invalid search/economy/tool limits")
	}
	for side, f := range c.Profiles {
		if side != "challenger" && side != "defender" {
			return fmt.Errorf("unknown profile side %s", side)
		}
		seen := map[p.Pos]bool{}
		for _, s := range f.Weapons {
			if s.Pos.X < 0 || s.Pos.Y < 0 {
				return fmt.Errorf("negative weapon coordinate")
			}
			if s.Kind != "rocket" && s.Kind != "gatling" && s.Kind != "railgun" {
				return fmt.Errorf("invalid weapon %s", s.Kind)
			}
			if seen[s.Pos] {
				return fmt.Errorf("duplicate site")
			}
			seen[s.Pos] = true
		}
		for _, pos := range f.Walls {
			if pos.X < 0 || pos.Y < 0 {
				return fmt.Errorf("negative wall coordinate")
			}
			if seen[pos] {
				return fmt.Errorf("overlapping wall site")
			}
			seen[pos] = true
		}
	}
	return nil
}
func BuildGrid(r p.Request, mobiles bool) nav.Grid {
	g := staticGrid(r)
	if mobiles {
		for _, u := range r.Mobiles() {
			if g.In(u.Pos) {
				g.Block[g.ID(u.Pos)] = true
			}
		}
	}
	return g
}
func staticGrid(r p.Request) nav.Grid { // Do not infer team identity from optional enemy teamId.
	g := nav.New(r.Map.Width, r.Map.Height)
	for _, z := range r.Map.Zones {
		if z.Type != "land" && g.In(z.Pos) {
			g.Block[g.ID(z.Pos)] = true
		}
	}
	for _, u := range r.Our.Roles {
		if u.Health > 0 && !u.Mobile() {
			for _, q := range u.Cells() {
				if g.In(q) {
					g.Block[g.ID(q)] = true
				}
			}
		}
	}
	for _, u := range r.Enemy.Roles {
		if u.Health > 0 {
			for _, q := range u.Cells() {
				if g.In(q) {
					g.Block[g.ID(q)] = true
				}
			}
		}
	}
	for _, u := range r.Robots.Roles {
		if u.Health > 0 {
			g.Block[g.ID(u.Pos)] = true
		}
	}
	return g
}
func profile(r p.Request, c Config) Profile {
	if f, ok := c.Profiles[r.Our.Type]; ok && f.Verified {
		return f
	}
	if !c.ExperimentalDemoLayout {
		return Profile{}
	}
	var base *p.Role
	for i := range r.Our.Roles {
		if r.Our.Roles[i].Type == "station" {
			base = &r.Our.Roles[i]
			break
		}
	}
	if base == nil {
		return Profile{}
	} // Explicit opt-in hypothesis only; these are NOT official build zones.
	f := Profile{}
	kinds := []string{"gatling", "railgun", "rocket"}
	for _, q := range []p.Pos{{X: base.Pos.X - 1, Y: base.Pos.Y}, {X: base.Pos.X, Y: base.Pos.Y + 1}, {X: base.Pos.X - 1, Y: base.Pos.Y + 1}} {
		if r.In(q) {
			f.Weapons = append(f.Weapons, Site{q, kinds[len(f.Weapons)]})
		}
	}
	for y := base.Pos.Y - 3; y <= base.Pos.Y+2; y++ {
		for x := base.Pos.X - 2; x <= base.Pos.X+3; x++ {
			if x != base.Pos.X-2 && x != base.Pos.X+3 && y != base.Pos.Y-3 && y != base.Pos.Y+2 {
				continue
			}
			if x == base.Pos.X+3 && y == base.Pos.Y-2 {
				continue
			}
			q := p.Pos{X: x, Y: y}
			if r.In(q) {
				f.Walls = append(f.Walls, q)
			}
		}
	}
	return f
}
func taskCells(r p.Request, t p.PlayerTask) []p.Pos {
	for _, z := range r.Map.Zones {
		if z.Pos == t.Pos {
			a := []p.Pos{}
			for _, v := range r.Map.Zones {
				if v.Type == z.Type {
					a = append(a, v.Pos)
				}
			}
			return a
		}
	}
	return []p.Pos{t.Pos}
}
