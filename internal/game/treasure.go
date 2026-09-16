package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
	"sort"
)

type TreasureAttempt struct {
	Round int      `json:"round"`
	Code  int      `json:"code"`
	Plan  Treasure `json:"plan"`
}

func treasureRejected(m Memory, t Treasure) bool {
	for _, a := range m.TreasureHistory {
		if a.Plan.Target != t.Target {
			continue
		}
		if a.Code == 1 || a.Code == 4 {
			return true
		}
		if a.Code == 2 && t.Latest <= a.Round {
			return true
		}
		if a.Code == 3 && sameItems(a.Plan.Items, t.Items) {
			return true
		}
	}
	return false
}
func sameItems(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a = append([]string(nil), a...)
	b = append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func planTreasure(r p.Request, u p.Role, g nav.Grid, c Config, m *Memory, out *p.Response, goals map[int][]int, gold *int) bool {
	if !m.TreasureDone && m.Treasure != nil && m.Treasure.Confidence >= c.Strategy.TreasureConfidence && !treasureRejected(*m, *m.Treasure) {
		t := m.Treasure
		path := g.Shortest(g.ID(u.Pos), g.Around([]p.Pos{t.Target}))
		if len(path) == 0 || len(path)+roleReturnDistance(r, g, c, u, g.Pos(path[len(path)-1]))+c.ReturnBuffer >= r.DayLeft() {
			return false
		}
		if r.Round > t.Latest {
			m.Treasure = nil
		} else if r.Round >= t.Earliest-20 {
			need := map[string]int{}
			for _, s := range t.Items {
				need[s]++
			}
			names := make([]string, 0, len(need))
			for s := range need {
				names = append(names, s)
			}
			sort.Strings(names)
			for _, s := range names {
				if u.Count(s) < need[s] {
					shopping := g.Shortest(g.ID(u.Pos), g.Around(zones(r, "weaponShop")))
					if len(shopping) == 0 {
						return false
					}
					fromShop := g.Shortest(shopping[len(shopping)-1], g.Around([]p.Pos{t.Target}))
					missing := 0
					for item, n := range need {
						missing += max(0, n-u.Count(item))
					}
					if len(fromShop) == 0 || len(shopping)+len(fromShop)+missing+roleReturnDistance(r, g, c, u, g.Pos(fromShop[len(fromShop)-1]))+c.ReturnBuffer >= r.DayLeft() {
						return false
					}
					for _, item := range r.Shop {
						if item.Name == s && item.Price > 0 && *gold >= item.Price && !u.Full() {
							cmd := p.Command{Action: "buy", Name: s, Num: 1}
							if moveOr(r, u, g, zones(r, "weaponShop"), cmd, out, goals) {
								if near(u.Pos, zones(r, "weaponShop")) {
									*gold -= item.Price
								}
								return true
							}
						}
					}
					return false
				}
			}
			if r.Round >= t.Earliest {
				cmd := p.At("summonTreasure", t.Target)
				cmd.Item = t.Items
				if moveOr(r, u, g, []p.Pos{t.Target}, cmd, out, goals) {
					if near(u.Pos, []p.Pos{t.Target}) {
						m.LastTreasureRound = r.Round
					}
					return true
				}
			}
		}
	}
	return false
}
