package game

import (
	p "competition/internal/protocol"
	"strconv"
)

func useOwned(r p.Request, u p.Role, out *p.Response) bool {
	if u.Health < 100 && u.Count("Medicine") > 0 {
		out.Commands[strconv.Itoa(u.ID)] = p.Command{Action: "use", Name: "Medicine"}
		return true
	}
	for _, target := range r.Our.Roles {
		if target.Health <= 0 || !near(u.Pos, target.Cells()) {
			continue
		}
		name := ""
		switch target.Type {
		case "station":
			name = "StationUpgradeVoucher"
		case "gatling", "railgun", "rocket":
			name = "WeaponUpgradeVoucher"
		}
		if name != "" && target.Level >= 1 && target.Level < 3 {
			name += strconv.Itoa(target.Level)
			if u.Count(name) > 0 {
				cmd := p.At("use", target.Pos)
				cmd.Name = name
				out.Commands[strconv.Itoa(u.ID)] = cmd
				return true
			}
		}
		if target.Type == "wall" && target.Health < 400 && u.Count("WallFixer") > 0 {
			cmd := p.At("use", target.Pos)
			cmd.Name = "WallFixer"
			out.Commands[strconv.Itoa(u.ID)] = cmd
			return true
		}
	}
	return false
}
