package record

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type Located struct {
	Path  string
	Entry Entry
}

func List(dir string) ([]Located, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var result []Located
	for _, path := range paths {
		entry, e := Read(path)
		if e != nil {
			return nil, fmt.Errorf("%s: %w", path, e)
		}
		result = append(result, Located{path, entry})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Entry.Sequence < result[j].Entry.Sequence })
	return result, nil
}

// Export copies immutable records; no unpacking or arbitrary path traversal.
func Export(selected, destination string, before, after int) error {
	focus, err := Read(selected)
	if err != nil {
		return err
	}
	all, err := List(filepath.Dir(selected))
	if err != nil {
		return err
	}
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	z := zip.NewWriter(f)
	feedback := false
	hashes := map[string]string{}
	var firstErr error
	for _, item := range all {
		e := item.Entry
		if e.RunID != focus.RunID || e.Epoch != focus.Epoch || e.Round < focus.Round-before || e.Round > focus.Round+after {
			continue
		}
		if e.Round == focus.Round+1 {
			feedback = true
		}
		b, er := os.ReadFile(item.Path)
		if er != nil {
			firstErr = er
			break
		}
		name := filepath.Base(item.Path)
		w, er := z.Create(name)
		if er == nil {
			_, er = w.Write(b)
		}
		if er != nil {
			firstErr = er
			break
		}
		hashes[name] = Digest(json.RawMessage(b))
	}
	manifest := struct {
		Round    int               `json:"focusRound"`
		Feedback bool              `json:"feedbackComplete"`
		Build    string            `json:"build"`
		Files    map[string]string `json:"files"`
		Note     string            `json:"note"`
	}{focus.Round, feedback, focus.Build, hashes, "Original binary is referenced by build hash, not included. Records contain raw messages, configuration and full memory."}
	if firstErr == nil {
		b, _ := json.MarshalIndent(manifest, "", "  ")
		w, e := z.Create("manifest.json")
		if e == nil {
			_, e = w.Write(b)
		}
		firstErr = e
	}
	if e := z.Close(); firstErr == nil {
		firstErr = e
	}
	if e := f.Close(); firstErr == nil {
		firstErr = e
	}
	return firstErr
}
