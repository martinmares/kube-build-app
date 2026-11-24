package buildweb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

func publishPackageToArtifactory(ctx context.Context, target PackageOutput, address, checksum string, raw []byte) error {
	token, err := readArtifactToken(target.TokenFile)
	if err != nil {
		return err
	}
	client, err := artifactHTTPClient(target.CAFile)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	client.Timeout = 2 * time.Minute
	request := func(method string, body io.Reader) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, address, body)
		if err != nil {
			return nil, err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if method == http.MethodPut {
			req.Header.Set("Content-Type", "application/zip")
			req.Header.Set("If-None-Match", "*")
			req.Header.Set("X-Checksum-Sha256", checksum)
		}
		return client.Do(req)
	}
	verify := func() (bool, error) {
		response, err := request(http.MethodGet, nil)
		if err != nil {
			return false, fmt.Errorf("Artifactory package verification request failed")
		}
		defer response.Body.Close()
		switch response.StatusCode {
		case http.StatusNotFound:
			return false, nil
		case http.StatusOK:
			content, err := io.ReadAll(io.LimitReader(response.Body, int64(len(raw))+1))
			if err != nil {
				return false, fmt.Errorf("cannot read the remote ZIP")
			}
			if !bytes.Equal(content, raw) {
				return false, fmt.Errorf("remote ZIP differs from review; refusing to overwrite")
			}
			return true, nil
		default:
			return false, fmt.Errorf("Artifactory package verification returned HTTP %d", response.StatusCode)
		}
	}
	if present, err := verify(); err != nil || present {
		return err
	}

	// The repository must deny overwrite atomically, including uploads from other processes.
	response, putErr := request(http.MethodPut, bytes.NewReader(raw))
	if response != nil {
		response.Body.Close()
	}
	// Verify even if the PUT response was lost or another publisher won a race.
	if present, err := verify(); err != nil || present {
		return err
	}
	if putErr != nil {
		return fmt.Errorf("Artifactory package upload failed; retry the same review")
	}
	return fmt.Errorf("Artifactory package upload returned HTTP %d but the ZIP could not be verified", response.StatusCode)
}
