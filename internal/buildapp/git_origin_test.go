package buildapp

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitInputOriginBindsCapturedBytes(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "release.yaml")
	raw := []byte("synthetic Git release")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	origin := GitOrigin{Repository: root, Tag: "release/DEMO", Commit: strings.Repeat("a", 40), Path: "releases/DEMO.yaml", SHA256: fmt.Sprintf("%x", sha256.Sum256(raw))}
	opts := Options{EvidenceDir: filepath.Join(t.TempDir(), "evidence"), InputGitOrigins: map[string]GitOrigin{file: origin}}
	r, err := newInputRecorder(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.read(file); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	same, err := r.read(file)
	if err != nil || string(same) != string(raw) {
		t.Fatal("repeat read changed")
	}
	opts.EvidenceDir = filepath.Join(t.TempDir(), "second")
	r, err = newInputRecorder(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.read(file); err == nil {
		t.Fatal("changed Git bytes accepted")
	}
	opts.EvidenceDir = filepath.Join(t.TempDir(), "conflicting")
	opts.InputOrigins = map[string]string{file: file}
	if _, err := newInputRecorder(opts); err == nil {
		t.Fatal("ambiguous input origin accepted")
	}
}
