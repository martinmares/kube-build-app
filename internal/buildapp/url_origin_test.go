package buildapp

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestURLInputEvidenceCapturesActualBytesAndRejectsChanges(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "release.yaml")
	raw := []byte("original release bytes")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	origin := URLOrigin{URL: "https://example.invalid/release.yaml", SHA256: fmt.Sprintf("%x", sha256.Sum256(raw))}
	opts := Options{EvidenceDir: filepath.Join(dir, "evidence"), InputURLOrigins: map[string]URLOrigin{file: origin}}
	recorder, err := newInputRecorder(opts)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := recorder.read(file)
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatalf("read: %s %v", actual, err)
	}
	if err := os.WriteFile(file, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	actual, err = recorder.read(file)
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatal("did not retain first-read bytes")
	}
	opts.EvidenceDir = filepath.Join(dir, "new-evidence")
	changed, err := newInputRecorder(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := changed.read(file); err == nil {
		t.Fatal("changed content accepted under the old URL checksum")
	}
}
