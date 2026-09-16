package game

import (
	nav "competition/internal/navigation"
	p "competition/internal/protocol"
	"strconv"
)

func economy(r p.Request, u p.Role, g nav.Grid, c Config, out *p.Response, goals map[int][]int, gold *int, claimed map[p.Pos]bool, towers *int) {
	f := profile(r, c)
	if r.Daylight() && *towers < 3 && *gold >= 25 {
		for _, site := range f.Weapons {
			if claimed[site.Pos] || !g.Free(site.Pos) || !buildConnected(r, g, site.Pos) {
				continue
			}
			cmd := p.At("build", site.Pos)
			cmd.Name = site.Kind
			if moveOr(r, u, g, []p.Pos{site.Pos}, cmd, out, goals) {
				claimed[site.Pos] = true
				if near(u.Pos, []p.Pos{site.Pos}) {
					*gold -= 25
					*towers++
				}
				return
			}
		}
	}
	// Deliver owned vouchers before making another shop trip.
	for _, target := range r.Our.Roles {
		prefix := ""
		if target.Type == "station" {
			prefix = "StationUpgradeVoucher"
		} else if target.Weapon() {
			prefix = "WeaponUpgradeVoucher"
		}
		if prefix != "" && target.Health > 0 && target.Level >= 1 && target.Level < 3 {
			name := prefix + strconv.Itoa(target.Level)
			if u.Count(name) > 0 {
				cmd := p.At("use", target.Pos)
				cmd.Name = name
				if moveOr(r, u, g, target.Cells(), cmd, out, goals) {
					return
				}
			}
		}
	}
	if r.Daylight() && u.Count("stone") > 0 {
		for _, q := range f.Walls {
			if claimed[q] || !g.Free(q) || p.Distance(u.Pos, q) == 0 {
				continue
			}
			if !buildConnected(r, g, q) {
				continue
			}
			cmd := p.At("build", q)
			cmd.Name = "wall"
			if moveOr(r, u, g, []p.Pos{q}, cmd, out, goals) {
				claimed[q] = true
				return
			}
		}
	}
	// Sell highest-value held ore, keeping two stones for walls only when walls are configured.
	for _, s := range []string{"copper", "iron", "stone"} {
		n := u.Count(s)
		if s == "stone" && len(f.Walls) > 0 {
			n = max(0, n-2)
		}
		if n > 0 && (n >= c.MineBatch || u.Full() || near(u.Pos, zones(r, "vendor"))) {
			if moveOr(r, u, g, zones(r, "vendor"), p.Command{Action: "sell", Name: s, Num: n}, out, goals) {
				return
			}
		}
	}
	desired := ""
	for _, target := range r.Our.Roles {
		if target.Health > 0 && target.Type == "station" && target.Level >= 1 && target.Level < 3 {
			desired = "StationUpgradeVoucher" + strconv.Itoa(target.Level)
			break
		}
	}
	if desired == "" {
		for _, target := range r.Weapons() {
			if target.Level >= 1 && target.Level < 3 {
				desired = "WeaponUpgradeVoucher" + strconv.Itoa(target.Level)
				break
			}
		}
	}
	for _, item := range r.Shop {
		if item.Name == desired && u.Count(desired) == 0 && !u.Full() && item.Price > 0 && *gold-item.Price >= c.ReserveGold {
			if moveOr(r, u, g, zones(r, "weaponShop"), p.Command{Action: "buy", Name: desired, Num: 1}, out, goals) {
				if near(u.Pos, zones(r, "weaponShop")) {
					*gold -= item.Price
				}
				return
			}
		}
	}
	if u.Full() {
		return
	}
	best := -1.0
	var mine p.Pos
	for _, z := range r.Map.Zones {
		if z.Type != "stone" && z.Type != "iron" && z.Type != "copper" {
			continue
		}
		path := g.Shortest(g.ID(u.Pos), g.Around([]p.Pos{z.Pos}))
		if path == nil {
			continue
		}
		price := 1
		for _, v := range r.Vendor {
			if v.Name == z.Type {
				price = v.Price
			}
		}
		if z.Type == "stone" && len(f.Walls) > 0 && u.Count("stone") < 3 {
			price += 5
		}
		v := float64(price) / float64(len(path)+1)
		if v > best {
			best = v
			mine = z.Pos
		}
	}
	if best >= 0 {
		moveOr(r, u, g, []p.Pos{mine}, p.At("collect", mine), out, goals)
	}
}
