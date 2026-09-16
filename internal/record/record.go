package record

import (
	"competition/internal/game"
	p "competition/internal/protocol"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type Entry struct {
	Schema       int         `json:"schema"`
	RunID        string      `json:"runId"`
	Epoch        int         `json:"epoch"`
	Sequence     uint64      `json:"requestSeq"`
	Round        int         `json:"roundNo"`
	Build        string      `json:"build"`
	Config       game.Config `json:"config"`
	ConfigHash   string      `json:"configHash"`
	Request      []byte      `json:"requestRaw"`
	RequestHash  string      `json:"requestHash"`
	Response     []byte      `json:"responseRaw"`
	ResponseHash string      `json:"responseHash"`
	Before       game.Memory `json:"memoryBefore"`
	After        game.Memory `json:"memoryAfter"`
	BeforeHash   string      `json:"memoryBeforeHash"`
	AfterHash    string      `json:"memoryAfterHash"`
	Trace        game.Trace  `json:"trace"`
	Received     string      `json:"receivedUTC"`
	DurationUS   int64       `json:"durationUs"`
	DecisionUS   int64       `json:"decisionUs"`
	Outcome      string      `json:"outcome"`
	Error        string      `json:"error,omitempty"`
	Stack        string      `json:"stack,omitempty"`
	Written      int         `json:"writtenBytes"`
	WriteError   string      `json:"writeError,omitempty"`
}

func Digest(v any) string { b, _ := json.Marshal(v); return game.Hash(b) }
func (e *Entry) Seal() {
	e.Schema = 1
	e.RequestHash = game.Hash(e.Request)
	e.ResponseHash = game.Hash(e.Response)
	e.ConfigHash = Digest(e.Config)
	e.BeforeHash = Digest(e.Before)
	e.AfterHash = Digest(e.After)
}
func (e Entry) Verify() error {
	if e.Schema != 1 {
		return fmt.Errorf("unsupported schema")
	}
	if game.Hash(e.Request) != e.RequestHash || game.Hash(e.Response) != e.ResponseHash || Digest(e.Config) != e.ConfigHash || Digest(e.Before) != e.BeforeHash || Digest(e.After) != e.AfterHash {
		return fmt.Errorf("record hash mismatch")
	}
	if !json.Valid(e.Response) {
		return fmt.Errorf("invalid recorded response")
	}
	return nil
}
func Read(path string) (Entry, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return Entry{}, e
	}
	var rec Entry
	if e = json.Unmarshal(b, &rec); e != nil {
		return rec, e
	}
	return rec, rec.Verify()
}

type Recorder struct {
	dir     string
	queue   chan Entry
	wg      sync.WaitGroup
	Dropped atomic.Uint64
	mu      sync.RWMutex
	closed  bool
}

func New(dir string) (*Recorder, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	r := &Recorder{dir: dir, queue: make(chan Entry, 32)}
	r.wg.Add(1)
	go r.loop()
	return r, nil
}
func (r *Recorder) Submit(e Entry) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return
	}
	select {
	case r.queue <- e:
	default:
		r.Dropped.Add(1)
		slog.Error("repro_queue_overflow", "requestSeq", e.Sequence, "round", e.Round)
	}
}
func (r *Recorder) loop() {
	defer r.wg.Done()
	for e := range r.queue {
		b, err := json.Marshal(e)
		if err == nil {
			path := filepath.Join(r.dir, fmt.Sprintf("%06d-e%d-r%04d.json", e.Sequence, e.Epoch, e.Round))
			err = os.WriteFile(path+".tmp", b, 0600)
			if err == nil {
				err = os.Rename(path+".tmp", path)
			}
		}
		if err != nil {
			r.Dropped.Add(1)
			slog.Error("repro_write_failed", "requestSeq", e.Sequence, "error", err)
		}
	}
}
func (r *Recorder) Close() {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.queue)
	}
	r.mu.Unlock()
	r.wg.Wait()
	if r.Dropped.Load() > 0 {
		_ = os.WriteFile(filepath.Join(r.dir, "INCOMPLETE.txt"), []byte(fmt.Sprintf("%d records lost\n", r.Dropped.Load())), 0600)
	}
}
func RunID() string                { return time.Now().UTC().Format("20060102T150405.000000000") }
func Compare(a, b p.Response) bool { return Digest(a) == Digest(b) }
