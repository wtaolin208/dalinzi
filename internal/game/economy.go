package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
	"context"
)

type workContext struct {
	ctx  context.Context
	r    p.Request
	u    p.Role
	g    nav.Grid
	c    Config
	m    Memory
	gold int
}
type workSink func(p.Command, []p.Pos, float64, int, int, string)
type workModule func(workContext, workSink)

// Add a module here to contribute candidates; joint allocation and validation remain shared.
func economicModules() []workModule { return []workModule{supplyCandidates, economyCandidates} }
func supplyCandidates(w workContext, add workSink) {
	r, u, c, gold := w.r, w.u, w.c, w.gold
	if len(r.Weapons()) >= 3 {
		for _, item := range r.Shop {
			if item.Name != "Bomb" && item.Name != "DizzyWeapon" && item.Name != "WallFixer" && item.Name != "Medicine" {
				continue
			}
			stock := 0
			for _, role := range r.Mobiles() {
				stock += role.Count(item.Name)
			}
			reserve := c.ReserveGold
			if r.Day() == 10 {
				reserve = 0
			}
			if stock > 0 || u.Full() || item.Price <= 0 || gold-item.Price < reserve {
				continue
			}
			needed := false
			for _, risk := range assessStructures(r, c) {
				if (item.Name == "Bomb" || item.Name == "DizzyWeapon") && risk.HorizonDamage > 0 {
					needed = true
				}
			}
			if item.Name == "Medicine" {
				for _, role := range r.Mobiles() {
					if role.Health < 100 {
						needed = true
					}
				}
			}
			for _, wall := range r.Our.Roles {
				if item.Name == "WallFixer" && wall.Type == "wall" && float64(wall.Health) < float64(maxStructureHP(wall))*c.Strategy.WallRepairHpRatio {
					needed = true
				}
			}
			if needed {
				add(p.Command{Action: "buy", Name: item.Name, Num: 1}, zones(r, "weaponShop"), 60, item.Price, 0, "supply:"+item.Name)
			}
		}
	}
}
func economyCandidates(w workContext, add workSink) {
	ctx, r, u, g, c, m, gold := w.ctx, w.r, w.u, w.g, w.c, w.m, w.gold
	f := profile(r, c)

	for _, kind := range []string{"copper", "iron", "stone"} {
		n := u.Count(kind)
		if kind == "stone" && len(f.Walls) > 0 {
			n = max(0, n-stoneReserve(r, c, u))
		}
		price := 0
		for _, item := range r.Vendor {
			if item.Name == kind {
				price = item.Price
			}
		}
		if n <= 0 || price <= 0 {
			continue
		}
		hold := r.Day() < 9 && marketTrend(r, c, m, kind) > 0 && !u.Full()
		required := requiredInvestment(r, c)
		if hold {
			n = minimumSale(gold, required, price, n)
			if n == 0 {
				continue
			}
		}
		if hold || r.Day() == 10 || n >= c.MineBatch || u.Full() || near(u.Pos, zones(r, "vendor")) {
			value := float64(30 + n*price)
			if hold {
				value += 500
			}
			exclusive := ""
			if hold {
				exclusive = "fund-required-investment"
			}
			add(p.Command{Action: "sell", Name: kind, Num: n}, zones(r, "vendor"), value, 0, 0, exclusive)
		}
	}
	if !u.Full() {
		for _, z := range r.Map.Zones {
			if ctx.Err() != nil {
				break
			}
			if z.Type != "stone" && z.Type != "iron" && z.Type != "copper" {
				continue
			}
			price := 1
			for _, item := range r.Vendor {
				if item.Name == z.Type {
					price = item.Price
				}
			}
			if z.Type == "stone" && len(f.Walls) > 0 && u.Count("stone") < 3 {
				price += 5
			}
			path := g.Shortest(g.ID(u.Pos), g.Around([]p.Pos{z.Pos}))
			if len(path) == 0 {
				continue
			}
			at := g.Pos(path[len(path)-1])
			sale := g.Shortest(g.ID(at), g.Around(zones(r, "vendor")))
			if len(sale) == 0 {
				// Missing market geometry: gathering is exploratory, not a proven sale cycle.
				if len(zones(r, "vendor")) == 0 {
					add(p.At("collect", z.Pos), []p.Pos{z.Pos}, float64(10*price), 0, 0, "")
				}
				continue
			}
			capacity := 100
			if u.Capacity != nil {
				capacity = *u.Capacity
			}
			quantity := min(c.MineBatch, capacity-len(u.Backpack))
			quantity = min(quantity, max(0, r.DayLeft()-(len(path)-1)-(len(sale)-1)-1-c.ReturnBuffer-roleReturnDistance(r, g, c, u, g.Pos(sale[len(sale)-1]))))
			if quantity <= 0 {
				continue
			}
			expected := float64(price) * (1 + float64(marketTrend(r, c, m, z.Type))*c.Strategy.ForecastPremium)
			cycle := (len(path) - 1) + quantity + (len(sale) - 1) + 1
			value := expected * float64(quantity) / float64(cycle)
			add(p.At("collect", z.Pos), []p.Pos{z.Pos}, value*float64(len(path)), 0, 0, "")
		}
	}
}
