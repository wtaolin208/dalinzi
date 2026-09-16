package game

import (
	p "competition/internal/protocol"
	"encoding/json"
	"testing"
)

func TestScoreUnknownIsNotZero(t *testing.T) {
	r := small()
	if err := json.Unmarshal([]byte(`{"totalScore":280}`), &r.Our); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{}`), &r.Enemy); err != nil {
		t.Fatal(err)
	}
	s := assessScore(r, DefaultConfig())
	if s.Enemy.Upper != nil || s.RiskMode != "even" {
		t.Fatal(s)
	}
	if err := json.Unmarshal([]byte(`{"totalScore":0}`), &r.Enemy); err != nil {
		t.Fatal(err)
	}
	if assessScore(r, DefaultConfig()).RiskMode != "lead" {
		t.Fatal("explicit zero was lost")
	}
	r.Enemy.Score = 400
	if assessScore(r, DefaultConfig()).RiskMode != "trail" {
		t.Fatal("observed deficit ignored")
	}
	for day, want := range map[int]int{1: 550, 7: 340, 8: 270, 9: 190, 10: 100} {
		if survivalRemaining(day) != want {
			t.Fatal(day)
		}
	}
}
func TestDayMilestonesAndLeadGate(t *testing.T) {
	r := small()
	c := DefaultConfig()
	for day := 1; day <= 10; day++ {
		r.Round = (day-1)*130 + 1
		d := planDay(r, c)
		if d.AllowOffense != (day >= 4) || d.FreezeStructures != (day >= 8) {
			t.Fatal(d)
		}
		if day >= 7 && d.BaseLevel != 3 {
			t.Fatal(d)
		}
	}
	r.Our.ScoreKnown = true
	r.Enemy.ScoreKnown = true
	r.Our.Score = 200
	if planDay(r, c).AllowOffense {
		t.Fatal("late lead allowed pressure")
	}
	r.Our.Roles = append(r.Our.Roles, p.Role{ID: 99, Type: "station", Level: 1, Health: 1500})
	r.Round = 261
	if upgradePriority(r, r.Our.Roles[len(r.Our.Roles)-1], planDay(r, c), c) <= 0 {
		t.Fatal("first upgrade not prioritized")
	}
}
func TestSafeFirePrefersKillDensity(t *testing.T) {
	r := small()
	r.Our.Roles = nil
	r.Round = 80
	r.Robots.Roles = []p.Robot{{Type: "middleRobot", Health: 60}, {Type: "largeRobot", Health: 500}}
	if damageValue(r, []int{20, 0}) <= damageValue(r, []int{0, 20}) {
		t.Fatal("safe damage ignores kill density")
	}
}
