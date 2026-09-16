package iteration

import (
	"archive/zip"
	"bytes"
	"competition/internal/game"
	p "competition/internal/protocol"
	"competition/internal/record"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fake struct {
	calls []Request
	reply func(Request) (Response, error)
}

func (f *fake) Call(_ context.Context, q Request) (Response, error) {
	f.calls = append(f.calls, q)
	return f.reply(q)
}
func candidate(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("run.sh")
	w.Write([]byte("test fixture"))
	z.Close()
	artifact := filepath.Join(root, "source.zip")
	if err := os.WriteFile(artifact, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "candidate")
	if err := Initialize(dir, artifact, time.Hour); err != nil {
		t.Fatal(err)
	}
	return dir
}
func entry(t *testing.T) Log {
	t.Helper()
	r := p.Request{Round: 1, Map: p.MapInfo{Width: 5, Height: 5}, Our: p.Team{ID: "a", Type: "challenger"}}
	raw, _ := json.Marshal(r)
	c := game.DefaultConfig()
	out, after, tr := (game.Engine{Config: c}).Decide(t.Context(), r, game.Memory{})
	response, _ := json.Marshal(out)
	e := record.Entry{Sequence: 1, Round: 1, Outcome: "decided", Config: c, Request: raw, Response: response, After: after, Trace: tr}
	e.Seal()
	data, _ := json.Marshal(e)
	return Log{Name: "../../ignored.json", Data: data, SHA256: game.Hash(data)}
}
func TestLifecycle(t *testing.T) {
	dir := candidate(t)
	l := entry(t)
	f := &fake{reply: func(q Request) (Response, error) {
		r := Response{VersionID: "v1", MatchID: "m1", SHA256: q.SHA256}
		switch q.Action {
		case "upload", "start_match":
			r.Status = "found"
		case "get_version":
			r.Status = "ready"
		case "get_match":
			r.Status = "finished"
			r.FullMatch = true
			r.Result = "win"
		case "download_logs":
			r.Logs = []Log{l}
		default:
			t.Fatalf("unexpected %s", q.Action)
		}
		return r, nil
	}}
	for _, want := range []string{"Uploaded", "BuildPassed", "MatchRunning", "MatchFinished", "Inconclusive"} {
		s, err := Step(t.Context(), dir, f)
		if err != nil || s.Phase != want {
			t.Fatalf("want %s got %+v %v", want, s, err)
		}
	}
	if len(f.calls) != 5 {
		t.Fatal(f.calls)
	}
	if _, err := os.Stat(filepath.Join(dir, "review.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Step(t.Context(), dir, f); err != nil || len(f.calls) != 5 {
		t.Fatal("terminal state made more calls")
	}
}
func TestUnknownSubmissionOnlyLooksUp(t *testing.T) {
	for _, phase := range []string{"Prepared", "BuildPassed"} {
		t.Run(phase, func(t *testing.T) {
			dir := candidate(t)
			s, _ := Load(dir)
			s.Phase = phase
			s.VersionID = "v1"
			save(dir, s)
			f := &fake{reply: func(q Request) (Response, error) {
				return Response{}, errors.New("connection lost after platform accepted")
			}}
			if _, err := Step(t.Context(), dir, f); err == nil {
				t.Fatal("expected timeout")
			}
			first := f.calls[0]
			f.reply = func(q Request) (Response, error) {
				return Response{Status: "found", VersionID: "v1", MatchID: "m1", SHA256: q.SHA256}, nil
			}
			if _, err := Step(t.Context(), dir, f); err != nil {
				t.Fatal(err)
			}
			want := "lookup_upload"
			if phase == "BuildPassed" {
				want = "lookup_match"
			}
			if f.calls[1].Action != want || f.calls[1].OperationID != first.OperationID {
				t.Fatal(f.calls)
			}
		})
	}
}
func TestBuildFailureAndVersionMismatch(t *testing.T) {
	for _, bad := range []bool{false, true} {
		dir := candidate(t)
		s, _ := Load(dir)
		s.Phase = "Uploaded"
		s.VersionID = "v1"
		save(dir, s)
		f := &fake{reply: func(q Request) (Response, error) {
			v := "v1"
			if bad {
				v = "other"
			}
			return Response{Status: "failed", VersionID: v, SHA256: q.SHA256}, nil
		}}
		s, err := Step(t.Context(), dir, f)
		want := "BuildFailed"
		if bad {
			want = "VersionMismatch"
		}
		if err != nil || s.Phase != want {
			t.Fatalf("%+v %v", s, err)
		}
		Step(t.Context(), dir, f)
		if len(f.calls) != 1 {
			t.Fatal("started match after failed build")
		}
	}
}
func TestLogsCannotMasqueradeAsSuccess(t *testing.T) {
	for _, data := range [][]byte{[]byte("<html>login</html>"), []byte(`{"error":"expired"}`)} {
		dir := candidate(t)
		s, _ := Load(dir)
		s.Phase = "MatchFinished"
		s.VersionID = "v"
		s.MatchID = "m"
		save(dir, s)
		f := &fake{reply: func(q Request) (Response, error) {
			return Response{VersionID: "v", MatchID: "m", Logs: []Log{{Data: data, SHA256: game.Hash(data)}}}, nil
		}}
		s, err := Step(t.Context(), dir, f)
		if err != nil || s.Phase != "LogsIncomplete" {
			t.Fatalf("%+v %v", s, err)
		}
	}
}
func TestBudgetArtifactAndLock(t *testing.T) {
	f := &fake{reply: func(q Request) (Response, error) { t.Fatal("must not call platform"); return Response{}, nil }}
	t.Run("expired", func(t *testing.T) {
		dir := candidate(t)
		s, _ := Load(dir)
		s.Deadline = time.Now().Add(-time.Second)
		save(dir, s)
		s, err := Step(t.Context(), dir, f)
		if err != nil || s.Phase != "Stopped" {
			t.Fatalf("%+v %v", s, err)
		}
	})
	t.Run("tampered", func(t *testing.T) {
		dir := candidate(t)
		s, _ := Load(dir)
		os.WriteFile(s.Artifact, []byte("changed"), 0600)
		if _, err := Step(t.Context(), dir, f); err == nil {
			t.Fatal("hash not checked")
		}
	})
	t.Run("locked", func(t *testing.T) {
		dir := candidate(t)
		os.WriteFile(filepath.Join(dir, "runner.lock"), nil, 0600)
		if _, err := Step(t.Context(), dir, f); err == nil {
			t.Fatal("lock ignored")
		}
	})
}
