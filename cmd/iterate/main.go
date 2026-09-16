package main

import (
	"competition/internal/iteration"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	dir := flag.String("dir", "", "unique candidate state directory")
	artifact := flag.String("init", "", "freeze an existing tested ZIP and initialize; does not upload")
	adapter := flag.String("adapter", "", "official platform adapter executable")
	step := flag.Bool("step", false, "perform at most one external operation")
	run := flag.Bool("run", false, "run one candidate through one complete match within its saved deadline")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "-dir is required")
		os.Exit(2)
	}
	if (*step || *run) && *adapter == "" {
		fmt.Fprintln(os.Stderr, "-adapter is required for platform operations")
		os.Exit(2)
	}
	if *step && *run {
		fmt.Fprintln(os.Stderr, "choose -step or -run")
		os.Exit(2)
	}
	var err error
	if *artifact != "" {
		err = iteration.Initialize(*dir, *artifact, time.Hour)
	}
	if err == nil && *step {
		_, err = iteration.Step(context.Background(), *dir, iteration.Process{Executable: *adapter, Args: flag.Args()})
	}
	if err == nil && *run {
		err = runCandidate(*dir, iteration.Process{Executable: *adapter, Args: flag.Args()})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	s, err := iteration.Load(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(s)
}

func runCandidate(dir string, a iteration.Adapter) error {
	s, err := iteration.Load(dir)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithDeadline(ctx, s.Deadline)
	defer cancel()
	delay := 5 * time.Second
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		old := s.Phase
		s, err = iteration.Step(ctx, dir, a)
		if err != nil {
			return err
		}
		if s.Error != "" {
			return fmt.Errorf("%s: %s", s.Phase, s.Error)
		}
		switch s.Phase {
		case "Inconclusive", "BuildFailed", "VersionMismatch", "Stopped":
			return nil
		}
		if s.Phase != old {
			_ = json.NewEncoder(os.Stdout).Encode(s)
			delay = 5 * time.Second
		} else {
			delay = min(30*time.Second, delay*2)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
