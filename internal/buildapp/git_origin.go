package buildapp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// GitOrigin describes captured bytes from a pinned commit, not a working tree.
type GitOrigin struct {
	URL            string `json:"url,omitempty"`
	Repository     string `json:"repository,omitempty"`
	Tag            string `json:"tag"`
	Commit         string `json:"commit"`
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
}

func (o GitOrigin) Validate() error {
	commit, err := hex.DecodeString(o.Commit)
	if err != nil || (len(commit) != 20 && len(commit) != 32) || o.Tag == "" || strings.ContainsAny(o.Tag, "\x00\r\n") || o.Path == "" || o.Path == "." || o.Path == ".." || strings.HasPrefix(o.Path, "/") || strings.HasPrefix(o.Path, "../") || path.Clean(o.Path) != o.Path || strings.ContainsAny(o.Path, "\\\x00\r\n") {
		return fmt.Errorf("invalid captured Git origin")
	}
	if o.URL != "" {
		if o.Repository != "" {
			return fmt.Errorf("ambiguous Git source origin")
		}
		if err := ValidateGitURL(o.URL); err != nil {
			return err
		}
	} else if !filepath.IsAbs(o.Repository) {
		return fmt.Errorf("invalid source repository origin")
	}
	for _, value := range []string{o.SHA256, o.ExpectedSHA256} {
		if value == "" {
			continue
		}
		raw, err := hex.DecodeString(value)
		if err != nil || len(raw) != sha256.Size {
			return fmt.Errorf("invalid Git document checksum")
		}
	}
	if o.SHA256 == "" || (o.ExpectedSHA256 != "" && !strings.EqualFold(o.ExpectedSHA256, o.SHA256)) {
		return fmt.Errorf("Git document SHA256 mismatch")
	}
	return nil
}
func (o GitOrigin) Verify(raw []byte) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != strings.ToLower(o.SHA256) {
		return fmt.Errorf("captured document differs from its Git origin")
	}
	return nil
}

// ValidateGitURL accepts direct source URLs without embedded passwords.
func ValidateGitURL(address string) error {
	u, err := url.Parse(address)
	valid := err == nil && u.RawQuery == "" && u.Fragment == "" && u.Opaque == "" && !strings.ContainsAny(address, "\x00\r\n")
	if valid {
		switch u.Scheme {
		case "https":
			valid = u.Host != "" && u.User == nil
		case "ssh":
			password := false
			if u.User != nil {
				_, password = u.User.Password()
			}
			valid = u.Host != "" && !password
		case "file":
			valid = u.Host == "" && u.User == nil && filepath.IsAbs(u.Path)
		default:
			valid = false
		}
	}
	if !valid {
		return fmt.Errorf("Git source URL must use https://, ssh:// or file:// without passwords, query or fragment")
	}
	return nil
}
