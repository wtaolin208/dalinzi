package main

import (
	"competition/internal/game"
	p "competition/internal/protocol"
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

func main() {
	config := flag.String("config", "configs/default.json", "candidate config")
	request := flag.String("request", "docs/request.txt", "official initial snapshot for one side")
	flag.Parse()
	c, err := game.LoadConfig(*config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	b, err := os.ReadFile(*request)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	r, err := p.Decode(b)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	issues := c.Readiness(r)
	_ = json.NewEncoder(os.Stdout).Encode(struct {
		Side   string   `json:"side"`
		Ready  bool     `json:"localReady"`
		Issues []string `json:"issues"`
		Note   string   `json:"note"`
	}{r.Our.Type, len(issues) == 0, issues, "Checks local configuration only; does not certify official build zones or battle results. Run for both sides."})
	if len(issues) > 0 {
		os.Exit(1)
	}
}
