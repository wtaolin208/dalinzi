// Package review replays recorded decisions without executing model or shell text.
package review

import (
	"competition/internal/game"
	p "competition/internal/protocol"
	"competition/internal/record"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

type Issue struct {
	Sequence uint64 `json:"sequence"`
	Round    int    `json:"round"`
	Kind     string `json:"kind"`
	Detail   string `json:"detail"`
}
type Report struct {
	Records             int     `json:"records"`
	Replayed            int     `json:"replayed"`
	ResponseDifferences int     `json:"responseDifferences"`
	MemoryDifferences   int     `json:"memoryDifferences"`
	P99US               int64   `json:"p99Us"`
	MaxUS               int64   `json:"maxUs"`
	Issues              []Issue `json:"issues"`
}

func Analyze(ctx context.Context, dir string) (Report, error) {
	var report Report
	all, err := record.List(dir)
	if err != nil {
		return report, err
	}
	if len(all) == 0 {
		return report, fmt.Errorf("no records")
	}
	var latencies []int64
	for _, item := range all {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		e := item.Entry
		report.Records++
		latencies = append(latencies, e.DurationUS)
		issue := func(kind, detail string) {
			report.Issues = append(report.Issues, Issue{e.Sequence, e.Round, kind, detail})
		}
		if e.Outcome != "decided" && e.Outcome != "cache_hit" {
			issue("service", e.Outcome)
		}
		r, err := p.Decode(e.Request)
		if err != nil {
			issue("decode", err.Error())
			continue
		}
		for _, x := range r.Errors {
			issue("judge", fmt.Sprintf("%d: %s", x.Code, x.Description))
		}
		keys := make([]string, 0, len(r.Results))
		for k := range r.Results {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !r.Results[k] {
				issue("previous_action_rejected", "role "+k+"; judge supplied no detailed cause")
			}
		}
		if e.WriteError != "" {
			issue("response_write", e.WriteError)
		}
		if e.Outcome != "decided" {
			continue
		}
		var expected p.Response
		if err = json.Unmarshal(e.Response, &expected); err != nil {
			issue("response_decode", err.Error())
			continue
		}
		if err = e.Config.Validate(); err != nil {
			issue("config", err.Error())
			continue
		}
		out, after, err := replay(ctx, e, r)
		if err != nil {
			issue("replay", err.Error())
			continue
		}
		report.Replayed++
		if record.Digest(out) != record.Digest(expected) {
			report.ResponseDifferences++
			issue("response_difference", "check binary version and deadline-limited search")
		}
		if record.Digest(after) != e.AfterHash {
			report.MemoryDifferences++
			issue("memory_difference", "check binary version and deadline-limited search")
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	report.MaxUS = latencies[len(latencies)-1]
	report.P99US = latencies[(len(latencies)*99+99)/100-1]
	return report, nil
}
func replay(ctx context.Context, e record.Entry, r p.Request) (out p.Response, after game.Memory, err error) {
	defer func() {
		if x := recover(); x != nil {
			err = fmt.Errorf("panic: %v", x)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 1400*time.Millisecond)
	defer cancel()
	out, after, _ = (game.Engine{Config: e.Config}).Decide(ctx, r, e.Before)
	return
}
