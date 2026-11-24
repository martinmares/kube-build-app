package buildweb

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kube-env/internal/buildapp"
)

func readGitRelease(ctx context.Context, repository, tag, document, checksum string) ([]byte, buildapp.GitOrigin, error) {
	return readGitDocument(ctx, repository, tag, document, checksum, false)
}

func readGitDocument(ctx context.Context, repository, tag, document, checksum string, bare bool) ([]byte, buildapp.GitOrigin, error) {
	var origin buildapp.GitOrigin
	root, err := filepath.Abs(repository)
	if err != nil {
		return nil, origin, fmt.Errorf("invalid source repository")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, origin, fmt.Errorf("source repository unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	run := func(args ...string) ([]byte, error) { return sourceGitCommand(ctx, root, nil, args...) }
	if bare {
		value, err := run("rev-parse", "--is-bare-repository")
		if err != nil || strings.TrimSpace(string(value)) != "true" {
			return nil, origin, fmt.Errorf("managed source must be a bare Git repository")
		}
	} else {
		top, err := run("rev-parse", "--show-toplevel")
		if err != nil || strings.TrimSpace(string(top)) != root {
			return nil, origin, fmt.Errorf("source repository must be a Git checkout root")
		}
	}
	ref := "refs/tags/" + tag
	if _, err = run("check-ref-format", ref); err != nil {
		return nil, origin, fmt.Errorf("invalid release tag")
	}
	commit, err := run("rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return nil, origin, fmt.Errorf("release tag does not resolve to a commit")
	}
	origin = buildapp.GitOrigin{Repository: root, Tag: tag, Commit: strings.TrimSpace(string(commit)), Path: document, ExpectedSHA256: strings.ToLower(checksum)}
	// All subsequent reads use the resolved commit and blob ID, never the mutable tag.
	entry, err := run("ls-tree", "-z", origin.Commit, "--", document)
	if err != nil {
		return nil, origin, err
	}
	parts := strings.SplitN(strings.TrimSuffix(string(entry), "\x00"), "\t", 2)
	if len(parts) != 2 || parts[1] != document {
		return nil, origin, fmt.Errorf("release document missing from tagged commit")
	}
	fields := strings.Fields(parts[0])
	if len(fields) != 3 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
		return nil, origin, fmt.Errorf("release document must be a regular Git file")
	}
	size, err := run("cat-file", "-s", fields[2])
	if err != nil {
		return nil, origin, err
	}
	count, err := strconv.ParseInt(strings.TrimSpace(string(size)), 10, 64)
	if err != nil || count <= 0 || count > 8<<20 {
		return nil, origin, fmt.Errorf("release must be nonempty and at most 8 MiB")
	}
	raw, err := run("cat-file", "blob", fields[2])
	if err != nil {
		return nil, origin, err
	}
	origin.SHA256 = fmt.Sprintf("%x", sha256.Sum256(raw))
	if err := origin.Verify(raw); err != nil {
		return nil, origin, err
	}
	return raw, origin, nil
}
