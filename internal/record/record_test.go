package record

import (
	"archive/zip"
	"competition/internal/game"
	"os"
	"path/filepath"
	"testing"
)

func TestIntegrityAndExport(t *testing.T) {
	dir := t.TempDir()
	rec, e := New(dir)
	if e != nil {
		t.Fatal(e)
	}
	for i := 1; i <= 3; i++ {
		entry := Entry{RunID: "test", Epoch: 1, Round: i, Sequence: uint64(i), Config: game.DefaultConfig(), Request: []byte(`{"x":1}`), Response: []byte(`{"roleCommandMap":{}}`)}
		entry.Seal()
		rec.Submit(entry)
	}
	rec.Close()
	all, e := List(dir)
	if e != nil || len(all) != 3 {
		t.Fatal(e, len(all))
	}
	entry := all[0].Entry
	entry.Request[1] = 'X'
	if entry.Verify() == nil {
		t.Fatal("tampering not detected")
	}
	dest := filepath.Join(t.TempDir(), "repro.zip")
	if e = Export(all[1].Path, dest, 20, 5); e != nil {
		t.Fatal(e)
	}
	z, e := zip.OpenReader(dest)
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	if len(z.File) != 4 {
		t.Fatal(len(z.File))
	}
	if e = Export(all[1].Path, dest, 20, 5); e == nil {
		t.Fatal("should not overwrite export")
	}
	if e = os.WriteFile(all[2].Path, []byte("broken"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = List(dir); e == nil {
		t.Fatal("bad record accepted")
	}
}
