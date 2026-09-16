// Package iteration persists a single candidate's platform lifecycle.
// Adapter operations are a local contract, not guessed remote HTTP endpoints.
package iteration

import (
	"archive/zip"
	"bytes"
	"competition/internal/game"
	"competition/internal/record"
	"competition/internal/review"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Request struct {
	Action      string `json:"action"`
	OperationID string `json:"operationId"`
	Artifact    string `json:"artifact"`
	SHA256      string `json:"sha256"`
	VersionID   string `json:"versionId"`
	MatchID     string `json:"matchId"`
}
type Log struct {
	Name   string `json:"name"`
	Data   []byte `json:"data"` // base64 in JSON; no archive extraction or remote filenames used locally
	SHA256 string `json:"sha256"`
}
type Response struct {
	Status    string `json:"status"`
	VersionID string `json:"versionId"`
	MatchID   string `json:"matchId"`
	SHA256    string `json:"sha256"`
	FullMatch bool   `json:"fullMatch"`
	Result    string `json:"result"`
	Logs      []Log  `json:"logs"`
}
type Adapter interface {
	Call(context.Context, Request) (Response, error)
}
type State struct {
	Schema        int       `json:"schema"`
	CandidateID   string    `json:"candidateId"`
	Phase         string    `json:"phase"`
	Artifact      string    `json:"artifact"`
	SHA256        string    `json:"sha256"`
	OperationID   string    `json:"operationId"`
	VersionID     string    `json:"versionId"`
	MatchID       string    `json:"matchId"`
	MatchAttempts int       `json:"matchAttempts"`
	Deadline      time.Time `json:"deadline"`
	Result        string    `json:"result"`
	Error         string    `json:"error,omitempty"`
}

// Initialize never overwrites an existing candidate. A new candidate gets a new directory.
func Initialize(dir, artifact string, duration time.Duration) error {
	if duration <= 0 || duration > time.Hour {
		return errors.New("duration must be in (0, 1h]")
	}
	b, err := os.ReadFile(artifact)
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return errors.New("empty artifact")
	}
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil || len(z.File) == 0 {
		return errors.New("artifact must be a nonempty ZIP")
	}
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		return err
	}
	if err = os.Mkdir(dir, 0700); err != nil {
		return err
	}
	abs, err := filepath.Abs(filepath.Join(dir, "artifact.zip"))
	if err != nil {
		return err
	}
	if err = os.WriteFile(abs, b, 0600); err != nil {
		return err
	}
	return save(dir, State{Schema: 1, CandidateID: hex.EncodeToString(id), Phase: "Prepared", Artifact: abs, SHA256: game.Hash(b), Deadline: time.Now().Add(duration)})
}
func Load(dir string) (State, error) {
	var s State
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(b, &s)
	if err == nil && s.Schema != 1 {
		err = errors.New("unsupported state schema")
	}
	return s, err
}
func save(dir string, s State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "state-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, filepath.Join(dir, "state.json"))
}
func event(dir string, action string, s State) error {
	f, err := os.OpenFile(filepath.Join(dir, "ledger.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = json.NewEncoder(f).Encode(struct {
		Time   time.Time `json:"time"`
		Action string    `json:"action"`
		State  State     `json:"state"`
	}{time.Now().UTC(), action, s}); err != nil {
		return err
	}
	return f.Sync()
}

// Step performs at most one external operation. Pending mutations are only queried
// after interruption or timeout, never blindly submitted a second time.
func Step(ctx context.Context, dir string, a Adapter) (State, error) {
	lock, err := os.OpenFile(filepath.Join(dir, "runner.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return State{}, fmt.Errorf("candidate locked (after a crash, verify no runner is active before removing runner.lock): %w", err)
	}
	lock.Close()
	defer os.Remove(filepath.Join(dir, "runner.lock"))
	s, err := Load(dir)
	if err != nil {
		return s, err
	}
	if s.Phase == "Inconclusive" || s.Phase == "BuildFailed" || s.Phase == "VersionMismatch" || s.Phase == "Stopped" {
		return s, nil
	}
	b, err := os.ReadFile(s.Artifact)
	if err != nil {
		return s, err
	}
	if game.Hash(b) != s.SHA256 {
		return s, errors.New("frozen artifact hash mismatch")
	}
	q := Request{Artifact: s.Artifact, SHA256: s.SHA256, VersionID: s.VersionID, MatchID: s.MatchID, OperationID: s.OperationID}
	mutation := false
	switch s.Phase {
	case "Prepared":
		q.Action = "upload"
		s.OperationID = "upload-" + s.CandidateID
		s.Phase = "UploadPending"
		mutation = true
	case "UploadPending":
		q.Action = "lookup_upload"
	case "Uploaded":
		q.Action = "get_version"
	case "BuildPassed":
		if s.MatchAttempts >= 1 {
			return s, errors.New("single-candidate match budget exhausted")
		}
		q.Action = "start_match"
		s.OperationID = "match-" + s.CandidateID
		s.Phase = "MatchPending"
		s.MatchAttempts++
		mutation = true
	case "MatchPending":
		q.Action = "lookup_match"
	case "MatchRunning":
		q.Action = "get_match"
	case "MatchFinished", "LogsIncomplete":
		q.Action = "download_logs"
	default:
		return s, fmt.Errorf("unknown phase %q", s.Phase)
	}
	if mutation {
		if !time.Now().Before(s.Deadline) {
			s.Phase = "Stopped"
			s.Error = "budget deadline reached before submission"
			return s, save(dir, s)
		}
		q.OperationID = s.OperationID
		if err = save(dir, s); err != nil {
			return s, err
		}
		if err = event(dir, "intent:"+q.Action, s); err != nil {
			return s, err
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r, callErr := a.Call(callCtx, q)
	if callErr != nil {
		s.Error = callErr.Error()
		if err = save(dir, s); err != nil {
			return s, err
		}
		return s, callErr
	}
	s.Error = ""
	switch q.Action {
	case "upload", "lookup_upload":
		if r.Status == "found" && r.VersionID != "" && r.SHA256 == s.SHA256 {
			s.VersionID = r.VersionID
			s.Phase = "Uploaded"
		} else {
			s.Error = "upload acceptance unknown; query again, do not resubmit"
		}
	case "get_version":
		if r.VersionID != s.VersionID || r.SHA256 != s.SHA256 {
			s.Phase = "VersionMismatch"
		} else if r.Status == "ready" {
			s.Phase = "BuildPassed"
		} else if r.Status == "failed" {
			s.Phase = "BuildFailed"
		} else if r.Status != "pending" {
			s.Error = "unknown build status"
		}
	case "start_match", "lookup_match":
		if r.Status == "found" && r.MatchID != "" && r.VersionID == s.VersionID {
			s.MatchID = r.MatchID
			s.Phase = "MatchRunning"
		} else {
			s.Error = "match acceptance unknown; query again, do not resubmit"
		}
	case "get_match":
		if r.MatchID != s.MatchID || r.VersionID != s.VersionID {
			s.Phase = "VersionMismatch"
		} else if r.Status == "finished" {
			if !r.FullMatch || (r.Result != "win" && r.Result != "draw" && r.Result != "loss") {
				s.Error = "missing full two-sided match result"
			} else {
				s.Phase = "MatchFinished"
				s.Result = r.Result
			}
		} else if r.Status == "failed" {
			s.Phase = "Stopped"
			s.Error = "platform match failed"
		} else if r.Status != "pending" {
			s.Error = "unknown match status"
		}
	case "download_logs":
		if r.MatchID != s.MatchID || r.VersionID != s.VersionID {
			s.Phase = "VersionMismatch"
		} else if err = verifyLogs(dir, r.Logs); err != nil {
			s.Phase = "LogsIncomplete"
			s.Error = err.Error()
		} else {
			s.Phase = "Inconclusive"
		}
	}
	if err = save(dir, s); err != nil {
		return s, err
	}
	if err = event(dir, "result:"+q.Action, s); err != nil {
		return s, err
	}
	return s, nil
}

func verifyLogs(dir string, logs []Log) error {
	if len(logs) == 0 {
		return errors.New("no logs returned")
	}
	entries := make([]record.Entry, len(logs))
	for i, l := range logs {
		if l.SHA256 == "" || game.Hash(l.Data) != l.SHA256 {
			return fmt.Errorf("log %d hash mismatch", i)
		}
		if err := json.Unmarshal(l.Data, &entries[i]); err != nil {
			return fmt.Errorf("log %d is not a recorded JSON turn: %w", i, err)
		}
		if err := entries[i].Verify(); err != nil {
			return fmt.Errorf("log %d: %w", i, err)
		}
	}
	// Data is never executed. File paths from the adapter are not trusted.
	logdir, err := os.MkdirTemp(dir, "logs-")
	if err != nil {
		return err
	}
	for i, l := range logs {
		if err = os.WriteFile(filepath.Join(logdir, fmt.Sprintf("%06d.json", i)), l.Data, 0600); err != nil {
			return err
		}
	}
	analysis, err := review.Analyze(context.Background(), logdir)
	if err != nil {
		return err
	}
	report := struct {
		LogDirectory string        `json:"logDirectory"`
		Analysis     review.Report `json:"analysis"`
		Conclusion   string        `json:"conclusion"`
	}{filepath.Base(logdir), analysis, "One match is insufficient for promotion. Full paired baseline evaluation remains required; replay differences can also result from deadline-limited search or different binary versions."}
	b, _ := json.MarshalIndent(report, "", "  ")
	return os.WriteFile(filepath.Join(dir, "review.json"), b, 0600)
}
