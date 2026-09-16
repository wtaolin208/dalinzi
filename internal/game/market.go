package game

import (
	p "competition/internal/protocol"
	"strings"
)

type MarketForecast struct {
	Resource      string  `json:"resource"`
	StartDay      int     `json:"startDay"`
	EndDay        int     `json:"endDay"`
	ExpectedTrend int     `json:"expectedTrend"`
	Confidence    float64 `json:"confidence"`
	EvidenceDay   int     `json:"evidenceDay"`
	Evidence      string  `json:"evidence"`
}

func acceptForecasts(r p.Request, m *Memory, items []MarketForecast) {
	for _, f := range items {
		if f.Resource != "stone" && f.Resource != "iron" && f.Resource != "copper" {
			continue
		}
		if f.StartDay < 1 || f.EndDay < f.StartDay || f.EndDay > 10 || f.EndDay < r.Day() || f.ExpectedTrend < -1 || f.ExpectedTrend > 1 || f.Confidence < 0 || f.Confidence > 1 || len(f.Evidence) < 4 {
			continue
		}
		evidence := false
		for _, n := range m.News {
			if n.Day == f.EvidenceDay && n.Day <= r.Day() && strings.Contains(n.News.Official, f.Evidence) {
				evidence = true
			}
		}
		if !evidence {
			continue
		}
		found := false
		for i, old := range m.Forecasts {
			if old.Resource == f.Resource && old.EvidenceDay == f.EvidenceDay {
				m.Forecasts[i] = f
				found = true
				break
			}
		}
		if !found {
			m.Forecasts = append(m.Forecasts, f)
		}
	}
	if len(m.Forecasts) > 30 {
		m.Forecasts = append([]MarketForecast(nil), m.Forecasts[len(m.Forecasts)-30:]...)
	}
}
func marketTrend(r p.Request, c Config, m Memory, kind string) int {
	if r.Day() >= 9 {
		return 0
	}
	trend, latest := 0, -1
	for _, f := range m.Forecasts {
		if f.Resource == kind && f.EndDay >= r.Day() && f.Confidence >= c.Strategy.ForecastConfidence && f.EvidenceDay >= latest {
			if f.StartDay > r.Day() {
				trend = f.ExpectedTrend
			} else {
				trend = 0
			}
			latest = f.EvidenceDay
		}
	}
	return trend
}
func stoneReserve(r p.Request, c Config, u p.Role) int {
	// Reserve across the team, assigned deterministically rather than per worker.
	if len(profile(r, c).Walls) == 0 {
		return 0
	}
	reserve := c.Strategy.StoneReserveMin
	if r.Day() < 10 && len(r.Weapons()) < 3 {
		reserve = c.Strategy.StoneReserveMax
	}
	for _, other := range r.Mobiles() {
		if other.ID == u.ID {
			return min(u.Count("stone"), reserve)
		}
		reserve = max(0, reserve-other.Count("stone"))
	}
	return 0
}
