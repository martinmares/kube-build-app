package buildapp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

type URLOrigin struct {
	URL            string `json:"url"`
	SHA256         string `json:"sha256"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
}

func ValidateDocumentURL(address string) error {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return fmt.Errorf("use a direct HTTPS URL without credentials, query or fragment")
	}
	return nil
}

func (o URLOrigin) Validate() error {
	if err := ValidateDocumentURL(o.URL); err != nil {
		return err
	}
	for _, value := range []string{o.SHA256, o.ExpectedSHA256} {
		if value == "" {
			continue
		}
		raw, err := hex.DecodeString(value)
		if err != nil || len(raw) != sha256.Size {
			return fmt.Errorf("document checksum must contain 64 hexadecimal characters")
		}
	}
	if o.SHA256 == "" {
		return fmt.Errorf("document origin is missing its checksum")
	}
	if o.ExpectedSHA256 != "" && !strings.EqualFold(o.SHA256, o.ExpectedSHA256) {
		return fmt.Errorf("document SHA256 mismatch")
	}
	return nil
}

func (o URLOrigin) Verify(raw []byte) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != strings.ToLower(o.SHA256) {
		return fmt.Errorf("captured document differs from its URL origin")
	}
	return nil
}
