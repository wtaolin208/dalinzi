package server

import (
	"competition/internal/game"
	p "competition/internal/protocol"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Synthetic snapshots exercise all day/night boundaries and side reset. This is
// a protocol lifecycle test, not a robot simulator or an official battle.
func TestTwoHalfSnapshotLifecycle(t *testing.T) {
	c := game.DefaultConfig()
	c.EnableNews = false
	h := New(c, nil, "lifecycle", "test")
	for _, side := range []string{"challenger", "defender"} {
		for round := 1; round <= 1300; round++ {
			r := p.Request{Round: round, Map: p.MapInfo{Width: 5, Height: 5, Zones: []p.Zone{{Pos: p.Pos{X: 2, Y: 1}, Type: "copper"}}}, Our: p.Team{ID: "team", Type: side, Roles: []p.Role{{ID: 1, Type: "worker", Health: 220, Pos: p.Pos{X: 1, Y: 1}}}}}
			b, _ := json.Marshal(r)
			var out p.Response
			if err := json.Unmarshal(send(h, b), &out); err != nil {
				t.Fatal(err)
			}
			want := ""
			if r.Daylight() {
				want = "collect"
			}
			if h.memory.Round != round || h.memory.Side != side || out.Commands["1"].Action != want {
				t.Fatalf("side %s round %d: %+v", side, round, out)
			}
		}
		if len(h.cache) != 1300 {
			t.Fatalf("cache not scoped to half: %d", len(h.cache))
		}
	}
	if h.epoch != 2 {
		t.Fatalf("epoch %d", h.epoch)
	}
}

func TestHealthAndMethod(t *testing.T) {
	h := New(game.DefaultConfig(), nil, "test", "test")
	for _, tc := range []struct {
		path   string
		status int
	}{{"/healthz", http.StatusOK}, {"/", http.StatusMethodNotAllowed}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.status {
			t.Fatal(w.Code)
		}
	}
}
