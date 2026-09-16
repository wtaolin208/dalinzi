package main

import (
	"competition/internal/game"
	"competition/internal/record"
	"competition/internal/server"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

var build = "development"

func main() {
	config := flag.String("config", "", "JSON config path")
	logs := flag.String("logs", "logs", "repro directory")
	debugLogs := flag.Bool("debug", true, "enable turn summaries and full replay logs; false keeps only warnings/errors")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: agent [-config config.json] [-debug=false] [-logs logs] port")
		os.Exit(2)
	}
	port, err := strconv.Atoi(flag.Arg(0))
	if err != nil || port < 1 || port > 65535 {
		fmt.Fprintln(os.Stderr, "invalid port")
		os.Exit(2)
	}
	if !*debugLogs {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	}
	c, err := game.LoadConfig(*config)
	if err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}
	run := record.RunID()
	if *debugLogs && build == "development" {
		if path, e := os.Executable(); e == nil {
			if b, e := os.ReadFile(path); e == nil {
				build = "sha256:" + game.Hash(b)
			}
		}
	}
	var rec *record.Recorder
	if *debugLogs {
		rec, err = record.New(filepath.Join(*logs, run))
		if err != nil {
			slog.Error("logs", "error", err)
			os.Exit(1)
		}
		defer rec.Close()
	}
	h := server.New(c, rec, run, build)
	srv := &http.Server{Addr: fmt.Sprintf("0.0.0.0:%d", port), Handler: h, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 4 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 64 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	slog.Info("listening", "address", srv.Addr, "runId", run, "build", build, "experimentalDemoLayout", c.ExperimentalDemoLayout)
	if err = srv.ListenAndServe(); err != http.ErrServerClosed {
		slog.Error("server", "error", err)
	}
}
