package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

type Pos struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func (p *Pos) UnmarshalJSON(b []byte) error {
	var raw struct {
		X *int `json:"x"`
		Y *int `json:"y"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if raw.X == nil || raw.Y == nil {
		return fmt.Errorf("position requires both x and y")
	}
	p.X = *raw.X
	p.Y = *raw.Y
	return nil
}

func Distance(a, b Pos) int { return max(abs(a.X-b.X), abs(a.Y-b.Y)) }
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

type Zone struct {
	Pos  Pos    `json:"pos"`
	Type string `json:"neutralType"`
}
type MapInfo struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Zones  []Zone `json:"zones"`
}
type Role struct {
	ID          int      `json:"id"`
	Pos         Pos      `json:"pos"`
	Type        string   `json:"roleType"`
	Health      int      `json:"health"`
	AttackPower int      `json:"attackPower"`
	AttackRange *int     `json:"attackRange,omitempty"`
	Capacity    *int     `json:"backPackCapability,omitempty"`
	Backpack    []string `json:"backpack"`
	Level       int      `json:"level"`
	Cooldown    *int     `json:"cooldown,omitempty"`
}

func (r Role) Mobile() bool { return r.Type == "worker" || r.Type == "pioneer" }
func (r Role) Weapon() bool { return r.Type == "gatling" || r.Type == "railgun" || r.Type == "rocket" }
func (r Role) Count(name string) int {
	n := 0
	for _, s := range r.Backpack {
		if s == name {
			n++
		}
	}
	return n
}
func (r Role) Full() bool {
	cap := 100
	if r.Type == "pioneer" {
		cap = 40
	}
	if r.Capacity != nil {
		cap = *r.Capacity
	}
	return len(r.Backpack) >= cap
}
func (r Role) Range() int {
	if r.AttackRange != nil {
		return *r.AttackRange
	}
	l := min(3, max(1, r.Level)) - 1
	switch r.Type {
	case "gatling":
		return []int{3, 5, 7}[l]
	case "railgun":
		return []int{6, 8, 10}[l]
	case "rocket":
		return []int{10, 15, 100000}[l]
	}
	return 0
}
func (r Role) Cells() []Pos {
	if r.Type == "station" {
		return []Pos{r.Pos, {X: r.Pos.X + 1, Y: r.Pos.Y}, {X: r.Pos.X, Y: r.Pos.Y - 1}, {X: r.Pos.X + 1, Y: r.Pos.Y - 1}}
	}
	return []Pos{r.Pos}
}

type PlayerTask struct {
	Type     string `json:"taskType"`
	Pos      Pos    `json:"taskPosition"`
	Cooldown int    `json:"coldDownRounds"`
	Score    int    `json:"scoreReward"`
	Gold     int    `json:"goldReward"`
	Valid    bool   `json:"isValid"`
	Timeout  *int   `json:"timeoutRounds,omitempty"`
}
type Team struct {
	Type       string       `json:"type"`
	ID         string       `json:"teamId"`
	Name       string       `json:"teamName"`
	Gold       int          `json:"goldNum"`
	Score      int          `json:"totalScore"`
	ScoreKnown bool         `json:"-"`
	Tasks      []PlayerTask `json:"playerTasks"`
	Roles      []Role       `json:"roles"`
}

// Missing enemy totals must never be interpreted as an observed zero.
func (t *Team) UnmarshalJSON(data []byte) error {
	type plain Team
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var presence struct {
		Score *int `json:"totalScore"`
	}
	if err := json.Unmarshal(data, &presence); err != nil {
		return err
	}
	*t = Team(value)
	t.ScoreKnown = presence.Score != nil
	return nil
}

func (t Team) MarshalJSON() ([]byte, error) {
	type plain Team
	var score *int
	if t.ScoreKnown || t.Score != 0 {
		value := t.Score
		score = &value
	}
	return json.Marshal(struct {
		plain
		Score *int `json:"totalScore,omitempty"`
	}{plain(t), score})
}

type Robot struct {
	ID     int    `json:"id"`
	Pos    Pos    `json:"pos"`
	Type   string `json:"roleType"`
	Health int    `json:"health"`
	State  string `json:"abnormalState"`
	Target string `json:"targetTeam"`
}
type RobotList struct {
	Roles []Robot `json:"roles"`
}
type News struct {
	Official string `json:"officialNews"`
	Folk     string `json:"folkLegends"`
}
type ShopItem struct {
	Name  string `json:"name"`
	Price int    `json:"price"`
}
type Error struct {
	Code        int    `json:"errorCode"`
	Description string `json:"description"`
}
type Request struct {
	Round          int             `json:"roundNo"`
	Map            MapInfo         `json:"mapInfo"`
	Our            Team            `json:"teamOur"`
	Enemy          Team            `json:"teamEnemy"`
	Robots         RobotList       `json:"robot"`
	PhaseTask      string          `json:"phaseTask"`
	Results        map[string]bool `json:"lastRoundRoleActionResults"`
	TreasureResult int             `json:"lastSummonTreasureResult"`
	LLM            string          `json:"llmResp"`
	News           News            `json:"worldNews"`
	CmdResult      string          `json:"lastCmdResult"`
	Vendor         []ShopItem      `json:"vendorShopList"`
	Shop           []ShopItem      `json:"weaponShopList"`
	Errors         []Error         `json:"errors"`
}

func (r Request) Day() int       { return (r.Round-1)/130 + 1 }
func (r Request) Daylight() bool { return (r.Round-1)%130 < 70 }
func (r Request) DayLeft() int   { return max(0, 70-(r.Round-1)%130) }
func (r Request) Mobiles() []Role {
	var a []Role
	for _, u := range r.Our.Roles {
		if u.Mobile() && u.Health > 0 {
			a = append(a, u)
		}
	}
	sort.Slice(a, func(i, j int) bool { return a[i].ID < a[j].ID })
	return a
}
func (r Request) Weapons() []Role {
	var a []Role
	for _, u := range r.Our.Roles {
		if u.Weapon() && u.Health > 0 {
			a = append(a, u)
		}
	}
	sort.Slice(a, func(i, j int) bool { return a[i].ID < a[j].ID })
	return a
}
func (r Request) In(p Pos) bool {
	return p.X >= 0 && p.Y >= 0 && p.X < r.Map.Width && p.Y < r.Map.Height
}
func Decode(raw []byte) (Request, error) {
	var r Request
	d := json.NewDecoder(bytes.NewReader(raw))
	if e := d.Decode(&r); e != nil {
		return r, e
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return r, fmt.Errorf("trailing JSON data")
	}
	if r.Round < 1 || r.Round > 1300 || r.Map.Width < 1 || r.Map.Height < 1 || r.Map.Width > 128 || r.Map.Height > 128 {
		return r, fmt.Errorf("invalid round or dimensions")
	}
	ids := map[int]bool{}
	for _, team := range []Team{r.Our, r.Enemy} {
		for _, u := range team.Roles {
			if ids[u.ID] || u.ID <= 0 {
				return r, fmt.Errorf("duplicate/invalid unit id %d", u.ID)
			}
			ids[u.ID] = true
			for _, p := range u.Cells() {
				if !r.In(p) {
					return r, fmt.Errorf("unit %d outside map", u.ID)
				}
			}
		}
	}
	for _, z := range r.Map.Zones {
		if !r.In(z.Pos) {
			return r, fmt.Errorf("zone outside map")
		}
	}
	for _, u := range r.Robots.Roles {
		if !r.In(u.Pos) {
			return r, fmt.Errorf("robot outside map")
		}
	}
	if len(r.Mobiles()) > 3 {
		return r, fmt.Errorf("more than three mobile roles")
	}
	return r, nil
}

type Command struct {
	Action     string   `json:"action"`
	Controller string   `json:"controllerId,omitempty"`
	Targets    []Pos    `json:"targetPos,omitempty"`
	Name       string   `json:"name,omitempty"`
	Num        int      `json:"num,omitempty"`
	Answer     *string  `json:"taskAnswer,omitempty"`
	Item       []string `json:"item,omitempty"`
}
type Response struct {
	Commands map[string]Command `json:"roleCommandMap"`
	Prompt   string             `json:"prompt"`
	Execute  string             `json:"executeCmd"`
}

func Empty() Response                 { return Response{Commands: map[string]Command{}} }
func At(action string, p Pos) Command { return Command{Action: action, Targets: []Pos{p}} }
