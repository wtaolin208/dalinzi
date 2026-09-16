package server

import (
	"competition/internal/game"
	p "competition/internal/protocol"
	"competition/internal/record"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync/atomic"
	"time"
)

type cached struct {
	hash string
	body []byte
}
type Handler struct {
	Engine       game.Engine
	Recorder     *record.Recorder
	RunID, Build string
	gate         chan struct{}
	memory       game.Memory
	cache        map[int]cached
	epoch        int
	seq          atomic.Uint64
	Budget       time.Duration
}

func New(c game.Config, rec *record.Recorder, runID, build string) *Handler {
	return &Handler{Engine: game.Engine{Config: c}, Recorder: rec, RunID: runID, Build: build, gate: make(chan struct{}, 1), cache: map[int]cached{}, epoch: 1, Budget: 1400 * time.Millisecond}
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodGet && req.URL.Path == "/healthz" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(req.Context(), h.Budget)
	defer cancel()
	rec := record.Entry{RunID: h.RunID, Build: h.Build, Sequence: h.seq.Add(1), Received: start.UTC().Format(time.RFC3339Nano), Config: h.Engine.Config}
	out := p.Empty()
	body, _ := json.Marshal(out)
	defer func() {
		rec.Response = body
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		n, e := w.Write(body)
		rec.Written = n
		if e != nil {
			rec.WriteError = e.Error()
		}
		rec.DurationUS = time.Since(start).Microseconds()
		if h.Recorder != nil {
			rec.Seal()
			h.Recorder.Submit(rec)
		}
		if rec.Outcome != "decided" && rec.Outcome != "cache_hit" || rec.WriteError != "" {
			slog.Warn("turn_failed", "round", rec.Round, "outcome", rec.Outcome, "error", rec.Error, "writeError", rec.WriteError)
		} else {
			slog.Info("turn", "round", rec.Round, "sequence", rec.Sequence, "outcome", rec.Outcome, "decisionUs", rec.DecisionUS, "durationUs", rec.DurationUS)
		}
	}()
	req.Body = http.MaxBytesReader(w, req.Body, 16<<20)
	raw, err := io.ReadAll(req.Body)
	rec.Request = raw
	if err != nil {
		rec.Outcome = "read_error"
		rec.Error = err.Error()
		return
	}
	r, err := p.Decode(raw)
	if err != nil {
		rec.Outcome = "decode_error"
		rec.Error = err.Error()
		return
	}
	rec.Round = r.Round
	select {
	case h.gate <- struct{}{}:
		defer func() { <-h.gate }()
	case <-ctx.Done():
		rec.Outcome = "queue_deadline"
		return
	}
	// Epoch reset is intentionally narrow. Arbitrary old turns cannot rewind state.
	if r.Our.ID != h.memory.Team || r.Our.Type != h.memory.Side || r.Round == 1 && h.memory.Round > 1 {
		if h.memory.Round > 0 {
			h.epoch++
		}
		h.memory = game.Memory{}
		h.cache = map[int]cached{}
	}
	rec.Epoch = h.epoch
	if h.Recorder != nil {
		rec.Before = game.CloneMemory(h.memory)
		rec.After = rec.Before
	}
	semantic := record.Digest(r)
	if old, ok := h.cache[r.Round]; ok {
		if old.hash == semantic {
			body = old.body
			rec.Outcome = "cache_hit"
		} else {
			rec.Outcome = "same_round_conflict"
			rec.Error = "different semantic input for a completed round"
		}
		return
	}
	if r.Round <= h.memory.Round {
		rec.Outcome = "stale_round"
		return
	}
	decisionStart := time.Now()
	response, after, tr, decisionErr, stack := h.safeDecide(ctx, r)
	rec.DecisionUS = time.Since(decisionStart).Microseconds()
	rec.Trace = tr
	if decisionErr != nil {
		rec.Outcome = "panic"
		rec.Error = decisionErr.Error()
		rec.Stack = stack
		return
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		rec.Outcome = "encode_error"
		rec.Error = err.Error()
		return
	}
	if req.Context().Err() != nil {
		rec.Outcome = "client_cancelled"
		return
	}
	h.memory = after
	if h.Recorder != nil {
		rec.After = game.CloneMemory(after)
	}
	body = encoded
	h.cache[r.Round] = cached{semantic, append([]byte(nil), body...)}
	rec.Outcome = "decided"
}
func (h *Handler) safeDecide(ctx context.Context, r p.Request) (out p.Response, after game.Memory, tr game.Trace, err error, stack string) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("decision panic: %v", v)
			stack = string(debug.Stack())
		}
	}()
	out, after, tr = h.Engine.Decide(ctx, r, h.memory)
	return
}
