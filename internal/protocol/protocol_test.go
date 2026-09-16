package protocol

import (
	"os"
	"testing"
)

func TestProvidedRequest(t *testing.T) {
	b, e := os.ReadFile("../../docs/request.txt")
	if e != nil {
		t.Fatal(e)
	}
	r, e := Decode(b)
	if e != nil {
		t.Fatal(e)
	}
	if r.Round != 85 || r.Daylight() || len(r.Mobiles()) != 3 {
		t.Fatal("bad request decode")
	}
	for _, w := range r.Weapons() {
		if w.Type == "gatling" && w.Range() != 4 {
			t.Fatal("runtime range must win")
		}
	}
}
func TestBoundaries(t *testing.T) {
	for _, x := range []struct {
		r   int
		day bool
	}{{1, true}, {70, true}, {71, false}, {130, false}, {131, true}, {200, true}, {201, false}, {1300, false}} {
		if (Request{Round: x.r}).Daylight() != x.day {
			t.Fatal(x)
		}
	}
	u := Role{Type: "station", Pos: Pos{3, 5}}
	if u.Cells()[3] != (Pos{4, 4}) {
		t.Fatal(u.Cells())
	}
}
func FuzzDecode(f *testing.F) {
	f.Add([]byte(`{"roundNo":1,"mapInfo":{"width":3,"height":3}}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		r, e := Decode(b)
		if e == nil && (!r.In(Pos{0, 0}) || r.Round < 1) {
			t.Fatal("accepted invalid request")
		}
	})
}
