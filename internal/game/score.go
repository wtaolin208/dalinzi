package game

import p "competition/internal/protocol"

// Upper == nil means unbounded uncertainty, not zero. Disappearing robots do
// not prove an enemy kill: fog, movement and dawn can produce the same snapshot.
type ScoreRange struct {
	Lower int  `json:"lower"`
	Upper *int `json:"upper"`
}
type ScoreAssessment struct {
	Our               ScoreRange `json:"our"`
	Enemy             ScoreRange `json:"enemy"`
	DailySurvival     int        `json:"dailySurvival"`
	RemainingSurvival int        `json:"remainingSurvival"`
	RiskMode          string     `json:"riskMode"`
	Reason            string     `json:"reason"`
}

func survivalRemaining(day int) int {
	day = min(10, max(1, day))
	return 550 - 5*(day-1)*day
}
func estimateTeamScore(t p.Team) ScoreRange {
	if t.ScoreKnown {
		score := t.Score
		return ScoreRange{score, &score}
	}
	return ScoreRange{Lower: 0}
}
func assessScore(r p.Request, c Config) ScoreAssessment {
	s := ScoreAssessment{Our: estimateTeamScore(r.Our), Enemy: estimateTeamScore(r.Enemy), DailySurvival: 10 * r.Day(), RemainingSurvival: survivalRemaining(r.Day()), RiskMode: "even", Reason: "unknown score interval; balanced policy"}
	if s.Enemy.Upper != nil && s.Our.Upper != nil {
		s.Reason = "observed totals with configured margin buffer"
		if s.Our.Lower-*s.Enemy.Upper > c.Strategy.ScoreMarginBuffer {
			s.RiskMode = "lead"
		}
		if s.Enemy.Lower-*s.Our.Upper > c.Strategy.ScoreMarginBuffer {
			s.RiskMode = "trail"
		}
	}
	return s
}
