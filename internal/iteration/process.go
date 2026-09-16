package iteration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"time"
)

// Process invokes an explicitly configured official-platform adapter directly,
// without a shell. Its credentials come from the inherited environment.
type Process struct {
	Executable string
	Args       []string
}
type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64<<20 {
		return 0, errors.New("adapter response exceeds 64 MiB")
	}
	return b.Buffer.Write(p)
}
func (p Process) Call(ctx context.Context, q Request) (Response, error) {
	var r Response
	if p.Executable == "" {
		return r, errors.New("official platform adapter is not configured")
	}
	b, err := json.Marshal(q)
	if err != nil {
		return r, err
	}
	cmd := exec.CommandContext(ctx, p.Executable, p.Args...)
	cmd.Stdin = bytes.NewReader(b)
	cmd.WaitDelay = time.Second
	var out limitedBuffer
	cmd.Stdout = &out
	// Do not persist stderr: adapters may accidentally echo credentials there.
	if err = cmd.Run(); err != nil {
		return r, err
	}
	d := json.NewDecoder(&out)
	if err = d.Decode(&r); err != nil {
		return r, err
	}
	if d.Decode(new(any)) != io.EOF {
		return r, errors.New("trailing adapter response")
	}
	return r, nil
}
