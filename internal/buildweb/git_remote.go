package buildweb

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kube-env/internal/buildapp"
	"kube-env/internal/pathlock"
)

type GitRemote struct {
	URL             string `yaml:"url"`
	RefreshInterval string `yaml:"refresh_interval,omitempty"`
	Username        string `yaml:"username,omitempty"`
	PasswordFile    string `yaml:"password_file,omitempty"`
	CAFile          string `yaml:"ca_file,omitempty"`
	cache           string
}

func (r GitRemote) Validate() error {
	if err := buildapp.ValidateGitURL(r.URL); err != nil {
		return err
	}
	u, _ := url.Parse(r.URL)
	if (r.Username == "") != (r.PasswordFile == "") || strings.ContainsAny(r.Username, ":\r\n\x00") {
		return fmt.Errorf("Git HTTPS username and password_file must be provided together")
	}
	if u.Scheme != "https" && (r.PasswordFile != "" || r.CAFile != "") {
		return fmt.Errorf("Git password_file and ca_file require HTTPS")
	}
	if r.RefreshInterval != "" {
		d, err := time.ParseDuration(r.RefreshInterval)
		if err != nil || d < time.Second {
			return fmt.Errorf("Git refresh_interval must be at least 1s, or omitted for on-demand reads")
		}
	}
	return nil
}
func (r GitRemote) identity() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(r.URL+"\n"+r.Username+"\n"+r.PasswordFile)))
}

type RefreshStatus struct {
	AttemptedAt time.Time `json:"attempted_at"`
	SucceededAt time.Time `json:"succeeded_at,omitempty"`
	Error       string    `json:"error,omitempty"`
}

var gitSourceLocks = pathlock.NewLockManager()

// The only credential-bearing values are private process environment entries.
// Neither command arguments, Git config files nor error messages contain them.
func sourceGitCommand(ctx context.Context, directory string, settings [][2]string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "--literal-pathspecs", "-C", directory, "-c", "core.hooksPath=" + os.DevNull}, args...)...)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") && !strings.HasPrefix(entry, "SSH_ASKPASS=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_NO_REPLACE_OBJECTS=1", "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=false", "SSH_ASKPASS=false", "GCM_INTERACTIVE=Never", "GIT_ALLOW_PROTOCOL=https:ssh:file")
	for _, name := range []string{"GIT_TRACE", "GIT_TRACE_PACKET", "GIT_TRACE_CURL", "GIT_CURL_VERBOSE", "GIT_TRACE2", "GIT_TRACE2_EVENT", "GIT_TRACE2_PERF"} {
		cmd.Env = append(cmd.Env, name+"=0")
	}
	settings = append([][2]string{{"http.followRedirects", "false"}, {"http.sslVerify", "true"}, {"core.sshCommand", "ssh -oBatchMode=yes -oStrictHostKeyChecking=yes"}}, settings...)
	cmd.Env = append(cmd.Env, "GIT_CONFIG_COUNT="+strconv.Itoa(len(settings)))
	for i, setting := range settings {
		cmd.Env = append(cmd.Env, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, setting[0]), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, setting[1]))
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("Git source operation failed; check source availability and access")
	}
	return out, nil
}

func (r GitRemote) environment() ([][2]string, error) {
	var settings [][2]string
	if r.CAFile != "" {
		settings = append(settings, [2]string{"http.sslCAInfo", r.CAFile})
	}
	if r.PasswordFile != "" {
		file, err := os.Open(r.PasswordFile)
		if err != nil {
			return nil, fmt.Errorf("cannot read Git password file")
		}
		raw, err := io.ReadAll(io.LimitReader(file, (16<<10)+1))
		_ = file.Close()
		password := strings.TrimSpace(string(raw))
		if err != nil || len(raw) > 16<<10 || password == "" || strings.ContainsAny(password, "\r\n\x00") {
			return nil, fmt.Errorf("cannot read a valid Git password file")
		}
		header := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(r.Username+":"+password))
		settings = append(settings, [2]string{"credential.helper", ""}, [2]string{"http.extraHeader", ""}, [2]string{"http." + r.URL + ".extraHeader", header})
	}
	return settings, nil
}

func (s *Server) sourceRefreshStatus() *RefreshStatus {
	if s.opts.ArtifactSource == nil || s.opts.ArtifactSource.Remote == nil {
		return nil
	}
	status := &RefreshStatus{}
	root, err := os.OpenRoot(s.opts.ArtifactSource.Remote.cache)
	if err != nil {
		return status
	}
	defer root.Close()
	raw, err := readBounded(root, "refresh.json", 16<<10)
	if err == nil {
		_ = json.Unmarshal(raw, status)
	}
	return status
}
func writeRefreshStatus(directory string, status RefreshStatus) error {
	raw, err := json.Marshal(status)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, "refresh-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(raw); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(directory, "refresh.json"))
}

func (s *Server) refreshRemoteGit(ctx context.Context, remote *GitRemote) (resultErr error) {
	// Caller holds the cache lock across refresh and any following blob read.
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := remote.Validate(); err != nil {
		return err
	}
	parent := filepath.Dir(remote.cache)
	if err := os.Mkdir(parent, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("managed Git parent must be an ordinary directory")
	}
	created := false
	if err := os.Mkdir(remote.cache, 0700); err == nil {
		created = true
	} else if !os.IsExist(err) {
		return err
	}
	info, err = os.Lstat(remote.cache)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("managed Git cache must be an ordinary directory")
	}
	owner := filepath.Join(remote.cache, "owner.json")
	if created {
		raw, _ := json.Marshal(struct {
			Identity string `json:"identity"`
		}{remote.identity()})
		if err := os.WriteFile(owner, raw, 0600); err != nil {
			return err
		}
	}
	info, err = os.Lstat(owner)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("Git cache has no ownership record")
	}
	root, err := os.OpenRoot(remote.cache)
	if err != nil {
		return err
	}
	raw, readErr := readBounded(root, "owner.json", 16<<10)
	_ = root.Close()
	var identity struct {
		Identity string `json:"identity"`
	}
	if readErr != nil || json.Unmarshal(raw, &identity) != nil || identity.Identity != remote.identity() {
		return fmt.Errorf("Git cache ownership does not match configured source")
	}
	status := *s.sourceRefreshStatus()
	status.AttemptedAt = time.Now().UTC()
	status.Error = ""
	defer func() {
		if resultErr != nil {
			status.Error = "Git refresh failed; check network, access and managed cache"
		} else {
			status.SucceededAt = time.Now().UTC()
		}
		if err := writeRefreshStatus(remote.cache, status); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("cannot record Git refresh status")
		}
	}()
	settings, err := remote.environment()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	repository := filepath.Join(remote.cache, "repository.git")
	if _, err := os.Lstat(repository); os.IsNotExist(err) {
		stage, err := os.MkdirTemp(remote.cache, "prepare-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		if _, err := sourceGitCommand(ctx, remote.cache, settings, "init", "--bare", "--template=", "--quiet", stage); err != nil {
			return err
		}
		if err := os.Rename(stage, repository); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	info, err = os.Lstat(repository)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("managed Git repository must be an ordinary directory")
	}
	bare, err := sourceGitCommand(ctx, repository, settings, "rev-parse", "--is-bare-repository")
	if err != nil || strings.TrimSpace(string(bare)) != "true" {
		return fmt.Errorf("managed Git copy must be a bare repository")
	}
	_, err = sourceGitCommand(ctx, repository, settings, "fetch", "--quiet", "--atomic", "--prune", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", remote.URL, "+refs/tags/*:refs/tags/*")
	return err
}

func (s *Server) readRemoteGit(ctx context.Context, tag, document, checksum string) ([]byte, buildapp.GitOrigin, error) {
	remote := s.opts.ArtifactSource.Remote
	unlock, err := gitSourceLocks.TryAcquire(remote.cache, "Git source read")
	if err != nil {
		return nil, buildapp.GitOrigin{}, fmt.Errorf("Git source is busy; retry shortly")
	}
	defer unlock()
	if err := s.refreshRemoteGit(ctx, remote); err != nil {
		return nil, buildapp.GitOrigin{}, err
	}
	raw, origin, err := readGitDocument(ctx, filepath.Join(remote.cache, "repository.git"), tag, document, checksum, true)
	if err != nil {
		return nil, origin, err
	}
	origin.Repository = ""
	origin.URL = remote.URL
	return raw, origin, origin.Verify(raw)
}

// StartSourceRefresh is opt-in and bound to the lifetime of the HTTP server.
// Every explicit read refreshes independently, even when a schedule is enabled.
func (s *Server) StartSourceRefresh(ctx context.Context, notify func(error)) {
	if s.opts.ArtifactSource == nil || s.opts.ArtifactSource.Remote == nil || s.opts.ArtifactSource.Remote.RefreshInterval == "" {
		return
	}
	remote := s.opts.ArtifactSource.Remote
	interval, err := time.ParseDuration(remote.RefreshInterval)
	if err != nil {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			unlock, err := gitSourceLocks.TryAcquire(remote.cache, "scheduled Git refresh")
			if err == nil {
				err = s.refreshRemoteGit(ctx, remote)
				unlock()
			}
			if notify != nil && ctx.Err() == nil {
				if err != nil {
					notify(fmt.Errorf("scheduled Git refresh failed; check source status"))
				} else {
					notify(nil)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
