package iteration

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestAdapterHelper(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != "iteration-adapter-helper" {
		return
	}
	var q Request
	if json.NewDecoder(os.Stdin).Decode(&q) != nil {
		os.Exit(2)
	}
	switch q.Action {
	case "bad":
		fmt.Print("not-json")
	case "trailing":
		fmt.Print("{} {}")
	case "fail":
		fmt.Fprintln(os.Stderr, "private adapter diagnostics")
		os.Exit(7)
	default:
		_ = json.NewEncoder(os.Stdout).Encode(Response{Status: "found", VersionID: q.OperationID})
	}
	os.Exit(0)
}

func TestProcessAdapterContract(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := Process{Executable: exe, Args: []string{"-test.run=^TestAdapterHelper$", "--", "iteration-adapter-helper"}}
	r, err := p.Call(t.Context(), Request{Action: "upload", OperationID: "literal $(not a shell)"})
	if err != nil || r.VersionID != "literal $(not a shell)" {
		t.Fatalf("%+v %v", r, err)
	}
	for _, action := range []string{"bad", "trailing", "fail"} {
		if _, err = p.Call(t.Context(), Request{Action: action}); err == nil {
			t.Fatal(action)
		}
	}
	if _, err = (Process{}).Call(t.Context(), Request{}); err == nil {
		t.Fatal("missing adapter accepted")
	}
}
