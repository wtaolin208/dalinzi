package server

import (
	"bytes"
	"competition/internal/game"
	p "competition/internal/protocol"
	"competition/internal/record"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
)

func request(round int) []byte {
	r := p.Request{Round: round, Map: p.MapInfo{Width: 5, Height: 5}, Our: p.Team{ID: "a", Type: "challenger", Roles: []p.Role{{ID: 1, Type: "worker", Health: 220, Pos: p.Pos{X: 1, Y: 1}}}}}
	b, _ := json.Marshal(r)
	return b
}
func send(h *Handler, b []byte) []byte {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/", bytes.NewReader(b)))
	return w.Body.Bytes()
}
func TestIdempotentConcurrentAndRecord(t *testing.T) {
	dir := t.TempDir()
	rec, e := record.New(dir)
	if e != nil {
		t.Fatal(e)
	}
	c := game.DefaultConfig()
	h := New(c, rec, "test", "test")
	want := send(h, request(1))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !bytes.Equal(send(h, request(1)), want) {
				t.Error("non-idempotent")
			}
		}()
	}
	wg.Wait()
	rec.Close()
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 9 {
		t.Fatal(len(files))
	}
	for _, f := range files {
		r, e := record.Read(f)
		if e != nil {
			t.Fatal(e)
		}
		if r.Outcome == "decided" {
			rr, _ := p.Decode(r.Request)
			out, after, _ := (game.Engine{Config: r.Config}).Decide(t.Context(), rr, r.Before)
			if record.Digest(after) != r.AfterHash {
				t.Fatal("memory replay mismatch")
			}
			var expected p.Response
			_ = json.Unmarshal(r.Response, &expected)
			if !record.Compare(out, expected) {
				t.Fatal("response mismatch")
			}
		}
	}
}
func TestBadRequestAndConflict(t *testing.T) {
	h := New(game.DefaultConfig(), nil, "test", "test")
	body := send(h, []byte(`{broken`))
	if !json.Valid(body) {
		t.Fatal(string(body))
	}
	send(h, request(2))
	r := request(2)
	var m map[string]any
	_ = json.Unmarshal(r, &m)
	m["phaseTask"] = "different"
	r, _ = json.Marshal(m)
	body = send(h, r)
	var out p.Response
	_ = json.Unmarshal(body, &out)
	if len(out.Commands) != 0 || out.Prompt != "" {
		t.Fatal("conflicting request processed")
	}
	if h.memory.Round != 2 {
		t.Fatal("memory mutated")
	}
}
