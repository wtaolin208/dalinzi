package review

import (
	"competition/internal/game"
	p "competition/internal/protocol"
	"competition/internal/record"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReviewIdentifiesFirstMismatchAndJudgeFeedback(t *testing.T) {
	dir := t.TempDir()
	c := game.DefaultConfig()
	r := p.Request{Round: 1, Map: p.MapInfo{Width: 5, Height: 5}, Our: p.Team{ID: "a", Type: "challenger"}, Results: map[string]bool{"1": false}}
	out, after, tr := (game.Engine{Config: c}).Decide(t.Context(), r, game.Memory{})
	out.Prompt = "changed output"
	raw, _ := json.Marshal(r)
	response, _ := json.Marshal(out)
	e := record.Entry{Sequence: 1, Round: 1, Config: c, Request: raw, Response: response, After: after, Trace: tr, Outcome: "decided", DurationUS: 1234}
	e.Seal()
	b, _ := json.Marshal(e)
	if err := os.WriteFile(filepath.Join(dir, "turn.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	report, err := Analyze(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if report.Replayed != 1 || report.ResponseDifferences != 1 || report.MemoryDifferences != 0 || report.P99US != 1234 || len(report.Issues) != 2 {
		t.Fatalf("%+v", report)
	}
}
