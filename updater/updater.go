// Package updater installs verified GitHub release artifacts for the CLI and desktop.
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Release builds set these with -ldflags. Local builds never replace themselves.
var (
	Version    = "dev"
	BuildTime  = "0" // Unix commit timestamp, not build or publication time.
	Repository = "prisma-ml-labs/maiku"
)

const maxDownload = 512 << 20

var ErrDevelopment = errors.New("automatic updates are unavailable in development builds")

type metadata struct {
	Version   string `json:"version"`
	BuildTime int64  `json:"buildTime"`
}

type release struct {
	Tag    string `json:"tag_name"`
	Draft  bool   `json:"draft"`
	Body   string `json:"body"`
	Assets []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

// Update is a checked release. Download locations cannot be supplied by callers.
type Update struct {
	Version     string
	asset, base string
	buildTime   int64
}

type Client struct {
	product, goos, arch, version, repository string
	buildTime                                int64
	api, downloads                           string
	http                                     *http.Client
	mu                                       sync.Mutex
	installed                                bool
}

func New(product string) *Client {
	t, _ := strconv.ParseInt(BuildTime, 10, 64)
	return &Client{product: product, goos: runtime.GOOS, arch: runtime.GOARCH,
		version: Version, buildTime: t, repository: Repository,
		api: "https://api.github.com", downloads: "https://github.com",
		http: &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return errors.New("insecure update redirect")
			}
			if len(via) >= 10 {
				return errors.New("too many update redirects")
			}
			return nil
		}},
	}
}

func (c *Client) Enabled() bool { return c.version != "dev" && c.buildTime > 0 }

func AutomaticEnabled() bool { return os.Getenv("MAIKU_AUTO_UPDATE") != "0" }

func assetName(product, goos, arch string) (string, error) {
	if arch != "amd64" && arch != "arm64" {
		return "", fmt.Errorf("unsupported architecture: %s", arch)
	}
	if product == "cli" {
		switch goos {
		case "darwin", "linux":
			return "maiku-cli-" + goos + "-" + arch + ".tar.gz", nil
		case "windows":
			return "maiku-cli-windows-" + arch + ".zip", nil
		}
	}
	if product == "desktop" {
		if goos == "darwin" {
			return "maiku-desktop-macos.zip", nil
		}
		if arch == "amd64" {
			if goos == "windows" {
				return "maiku-desktop-windows-amd64.exe", nil
			}
			if goos == "linux" {
				return "maiku-desktop-linux-amd64.tar.gz", nil
			}
		}
	}
	return "", fmt.Errorf("no %s release for %s/%s", product, goos, arch)
}

func (c *Client) get(ctx context.Context, address string, out io.Writer, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "maiku-updater/"+c.version)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("update server returned HTTP %d", resp.StatusCode)
	}
	n, err := io.Copy(out, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return errors.New("update exceeds size limit")
	}
	return nil
}

func (c *Client) json(ctx context.Context, address string, dest any) error {
	var b strings.Builder
	// The release list includes GitHub's full asset/uploader records for up to
	// 100 releases. Keep it bounded without rejecting an ordinary mature feed.
	if err := c.get(ctx, address, &b, 16<<20); err != nil {
		return err
	}
	return json.Unmarshal([]byte(b.String()), dest)
}

// Check includes commit prereleases, matching this repository's release workflow.
// Commit timestamps prevent an older build published later from downgrading an app.
func (c *Client) Check(ctx context.Context) (*Update, error) {
	if !c.Enabled() {
		return nil, ErrDevelopment
	}
	asset, err := assetName(c.product, c.goos, c.arch)
	if err != nil {
		return nil, err
	}
	var releases []release
	if err := c.json(ctx, c.api+"/repos/"+c.repository+"/releases?per_page=100", &releases); err != nil {
		return nil, err
	}
	var best *Update
	for _, r := range releases {
		if r.Draft || r.Tag == c.version {
			continue
		}
		found := map[string]bool{}
		for _, a := range r.Assets {
			found[a.Name] = true
		}
		if !found[asset] || !found["updater.json"] || !found["checksums.txt"] {
			continue
		}
		base := c.downloads + "/" + c.repository + "/releases/download/" + url.PathEscape(r.Tag) + "/"
		var m metadata
		// CI also embeds the small manifest in the release body. Reading all
		// candidates in one API request keeps startup fast even with many releases.
		const prefix = "<!-- maiku-updater:"
		start := strings.Index(r.Body, prefix)
		if start < 0 {
			continue
		}
		body := r.Body[start+len(prefix):]
		end := strings.Index(body, "-->")
		if end < 0 || json.Unmarshal([]byte(body[:end]), &m) != nil {
			return nil, errors.New("invalid release update metadata")
		}
		if m.Version != r.Tag {
			return nil, errors.New("release metadata version mismatch")
		}
		if m.BuildTime <= c.buildTime || (best != nil && m.BuildTime <= best.buildTime) {
			continue
		}
		best = &Update{Version: r.Tag, asset: asset, base: base, buildTime: m.BuildTime}
	}
	return best, nil
}

func checksum(data, name string) (string, error) {
	var result string
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != sha256.Size || result != "" {
			return "", errors.New("invalid or duplicate update checksum")
		}
		result = strings.ToLower(fields[0])
	}
	if result == "" {
		return "", errors.New("missing update checksum")
	}
	return result, nil
}

// Install replaces the executable (the complete .app on macOS). The running
// process continues uninterrupted; the new version is used on the next launch.
func (c *Client) Install(ctx context.Context, u *Update) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.installed {
		return errors.New("an update is already installed; restart maiku")
	}
	if !c.Enabled() {
		return ErrDevelopment
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	target, err := installTarget(exe, c.product, c.goos)
	if err != nil {
		return err
	}
	if err = c.install(ctx, u, target); err != nil {
		return err
	}
	c.installed = true
	return nil
}

func installTarget(exe, product, goos string) (string, error) {
	if product == "desktop" && goos == "darwin" {
		bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
		if filepath.Base(filepath.Dir(exe)) != "MacOS" || filepath.Base(filepath.Dir(filepath.Dir(exe))) != "Contents" || !strings.HasSuffix(bundle, ".app") {
			return "", errors.New("desktop updates require an installed macOS .app bundle")
		}
		return bundle, nil
	}
	return exe, nil
}

func (c *Client) install(ctx context.Context, u *Update, target string) error {
	name, err := assetName(c.product, c.goos, c.arch)
	if err != nil {
		return err
	}
	if u == nil || u.asset != name || u.buildTime <= c.buildTime || u.base != c.downloads+"/"+c.repository+"/releases/download/"+url.PathEscape(u.Version)+"/" {
		return errors.New("invalid update")
	}
	// A crash-safe OS lock serializes separate running instances as well.
	unlock, err := acquireLock(target + ".update-lock")
	if err != nil {
		return fmt.Errorf("cannot lock installation (another updater or unwritable directory): %w", err)
	}
	defer unlock()
	// A second, older process must not overwrite a version another process has
	// already installed. The receipt is written while holding the shared lock.
	receipt := target + ".update-version"
	if data, err := os.ReadFile(receipt); err == nil {
		var installed metadata
		if json.Unmarshal(data, &installed) == nil && installed.BuildTime >= u.buildTime {
			return errors.New("this update or a newer version is already installed; restart maiku")
		}
	}
	stage, err := os.MkdirTemp(filepath.Dir(target), ".maiku-update-")
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(stage)
		}
	}()
	var manifest metadata
	if err := c.json(ctx, u.base+"updater.json", &manifest); err != nil {
		return err
	}
	if manifest.Version != u.Version || manifest.BuildTime != u.buildTime {
		return errors.New("release manifest does not match the checked update")
	}
	var sums strings.Builder
	if err := c.get(ctx, u.base+"checksums.txt", &sums, 1<<20); err != nil {
		return err
	}
	want, err := checksum(sums.String(), name)
	if err != nil {
		return err
	}
	archive := filepath.Join(stage, "download")
	f, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	err = c.get(ctx, u.base+name, io.MultiWriter(f, hash), maxDownload)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if hex.EncodeToString(hash.Sum(nil)) != want {
		return errors.New("update checksum mismatch; installation was not changed")
	}
	payload, err := unpack(archive, stage, name, c.product, c.goos)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	backup := filepath.Join(stage, "previous")
	keep, err = replaceInstallation(target, payload, backup, os.Rename)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(manifest)
	receiptTemp := filepath.Join(stage, "receipt")
	if err := os.WriteFile(receiptTemp, data, 0600); err != nil {
		// The installation succeeded; a missing receipt must not roll it back.
		// Preserve the backup to allow recovery if the directory became unwritable.
		keep = true
	} else if err := os.Rename(receiptTemp, receipt); err != nil {
		keep = true
	}
	// Windows can rename a running executable, but cannot remove it until exit.
	// Preserve the backup if locked; never make installation depend on deletion.
	if !keep {
		if err := os.RemoveAll(backup); err != nil {
			keep = true
		}
	}
	// Even if Windows still holds the backup open, discard the downloaded
	// archive and extracted scaffolding rather than retaining two extra copies.
	_ = os.Remove(archive)
	_ = os.RemoveAll(filepath.Join(stage, "payload"))
	return nil
}

func replaceInstallation(target, payload, backup string, rename func(string, string) error) (keepBackup bool, err error) {
	if err := rename(target, backup); err != nil {
		return false, fmt.Errorf("cannot move current installation: %w", err)
	}
	if err := rename(payload, target); err != nil {
		if rollbackErr := rename(backup, target); rollbackErr != nil {
			return true, fmt.Errorf("install failed: %v; restore %s to %s manually: %w", err, backup, target, rollbackErr)
		}
		return false, fmt.Errorf("install failed; previous version restored: %w", err)
	}
	return false, nil
}
