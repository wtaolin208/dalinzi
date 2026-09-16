package main

import (
	"competition/internal/game"
	p "competition/internal/protocol"
	"competition/internal/record"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	verify := flag.Bool("verify", false, "only verify record integrity")
	timeline := flag.Bool("timeline", false, "summarize all records in a run directory")
	export := flag.String("export", "", "export selected record and nearby rounds to a new ZIP")
	timeout := flag.Duration("timeout", 30*time.Second, "replay time budget")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: replay [-verify] record.json")
		os.Exit(2)
	}
	if *timeline {
		entries, e := record.List(flag.Arg(0))
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		for _, item := range entries {
			r := item.Entry
			var input p.Request
			_ = json.Unmarshal(r.Request, &input)
			fmt.Printf("seq=%d epoch=%d round=%d outcome=%s mode=%s decisionUs=%d errors=%v rejected=%v\n", r.Sequence, r.Epoch, r.Round, r.Outcome, r.Trace.Mode, r.DecisionUS, game.ExplainErrors(input), r.Trace.Rejected)
		}
		return
	}
	if *export != "" {
		if err := record.Export(flag.Arg(0), *export, 20, 5); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("exported", *export)
		return
	}
	rec, err := record.Read(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *verify {
		fmt.Println("record integrity verified")
		return
	}
	if rec.Outcome != "decided" {
		fmt.Fprintf(os.Stderr, "record outcome %q is not a decision; inspect or use -verify\n", rec.Outcome)
		os.Exit(2)
	}
	r, err := p.Decode(rec.Request)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	out, after, tr := (game.Engine{Config: rec.Config}).Decide(ctx, r, rec.Before)
	var expected p.Response
	_ = json.Unmarshal(rec.Response, &expected)
	same := record.Compare(out, expected)
	memorySame := record.Digest(after) == rec.AfterHash
	report := struct {
		ResponseSame bool       `json:"responseSame"`
		MemorySame   bool       `json:"memorySame"`
		Response     p.Response `json:"response"`
		Trace        game.Trace `json:"trace"`
	}{same, memorySame, out, tr}
	b, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(b))
	if !same || !memorySame {
		fmt.Fprintln(os.Stderr, "replay differs: inspect trace, build version, and deadline-based termination; this run uses the recorded expansion limits with a new wall-clock budget")
		os.Exit(1)
	}
}
