package main

import (
	"competition/internal/review"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

func main() {
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: review log-directory")
		os.Exit(2)
	}
	r, err := review.Analyze(context.Background(), flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	_ = e.Encode(r)
	if len(r.Issues) > 0 {
		os.Exit(1)
	}
}
