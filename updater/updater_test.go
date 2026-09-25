package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAssetMatrix(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			name, err := assetName("cli", goos, arch)
			if err != nil || !strings.Contains(name, goos+"-"+arch) {
				t.Fatalf("%s/%s: %s, %v", goos, arch, name, err)
			}
		}
	}
	for _, tc := range []struct{ os, arch, want string }{
		{"darwin", "arm64", "maiku-desktop-macos.zip"},
		{"darwin", "amd64", "maiku-desktop-macos.zip"},
		{"windows", "amd64", "maiku-desktop-windows-amd64.exe"},
		{"linux", "amd64", "maiku-desktop-linux-amd64.tar.gz"},
	} {
		got, err := assetName("desktop", tc.os, tc.arch)
		if got != tc.want || err != nil {
			t.Fatalf("%+v: %q %v", tc, got, err)
		}
	}
	for _, tc := range [][3]string{{"desktop", "linux", "arm64"}, {"cli", "freebsd", "amd64"}, {"cli", "linux", "386"}, {"other", "linux", "amd64"}} {
		if _, err := assetName(tc[0], tc[1], tc[2]); err == nil {
			t.Fatalf("accepted unsupported %v", tc)
		}
	}
}

func tarball(t *testing.T, name, data string, kind byte) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	h := &tar.Header{Name: name, Mode: 0755, Size: int64(len(data)), Typeflag: kind}
	if kind == tar.TypeSymlink {
		h.Size = 0
		h.Linkname = "../../outside"
	}
	if err := tw.WriteHeader(h); err != nil {
		t.Fatal(err)
	}
	if h.Size > 0 {
		if _, err := tw.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func zipball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func fixture(t *testing.T, product, goos, arch string, data []byte) (*Client, *Update, *httptest.Server) {
	t.Helper()
	name, err := assetName(product, goos, arch)
	if err != nil {
		t.Fatal(err)
	}
	m := metadata{Version: "sha-new", BuildTime: 200}
	manifest, _ := json.Marshal(m)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases"):
			json.NewEncoder(w).Encode([]map[string]any{{"tag_name": m.Version, "body": "<!-- maiku-updater:" + string(manifest) + " -->", "assets": []map[string]string{{"name": name}, {"name": "checksums.txt"}, {"name": "updater.json"}}}})
		case strings.HasSuffix(r.URL.Path, "/updater.json"):
			w.Write(manifest)
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			fmt.Fprintf(w, "%x  %s\n", sha256.Sum256(data), name)
		case strings.HasSuffix(r.URL.Path, "/"+name):
			w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	c := New(product)
	c.goos, c.arch, c.version, c.buildTime = goos, arch, "sha-old", 100
	c.api, c.downloads, c.http = server.URL, server.URL, server.Client()
	u, err := c.Check(context.Background())
	if err != nil || u == nil {
		t.Fatalf("Check: %v %v", u, err)
	}
	return c, u, server
}

func TestCheckSelection(t *testing.T) {
	name, _ := assetName("cli", "linux", "amd64")
	record := func(tag string, stamp int, draft, complete bool) map[string]any {
		assets := []map[string]string{{"name": name}, {"name": "checksums.txt"}}
		if complete {
			assets = append(assets, map[string]string{"name": "updater.json"})
		}
		return map[string]any{"tag_name": tag, "draft": draft, "prerelease": true, "body": fmt.Sprintf(`<!-- maiku-updater:{"version":%q,"buildTime":%d} -->`, tag, stamp), "assets": assets}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{record("sha-ancient", 50, false, true), record("sha-draft", 900, true, true), record("sha-incomplete", 800, false, false), record("sha-new", 300, false, true), record("sha-middle", 200, false, true)})
	}))
	defer server.Close()
	c := New("cli")
	c.api, c.http, c.goos, c.arch, c.version, c.buildTime = server.URL, server.Client(), "linux", "amd64", "sha-old", 100
	u, err := c.Check(context.Background())
	if err != nil || u == nil || u.Version != "sha-new" {
		t.Fatalf("selection: %+v %v", u, err)
	}
	c.buildTime = 300
	if u, err := c.Check(context.Background()); u != nil || err != nil {
		t.Fatalf("downgrade: %+v %v", u, err)
	}
	c.version = "dev"
	if _, err := c.Check(context.Background()); !errors.Is(err, ErrDevelopment) {
		t.Fatal(err)
	}
}

func TestInstallArtifacts(t *testing.T) {
	for _, tc := range []struct {
		product, goos, arch string
		data                []byte
		inside              string
	}{
		{"cli", "linux", "amd64", tarball(t, "maiku", "new", tar.TypeReg), ""},
		{"cli", "darwin", "arm64", tarball(t, "maiku", "new", tar.TypeReg), ""},
		{"cli", "windows", "arm64", zipball(t, map[string]string{"maiku.exe": "new"}), ""},
		{"desktop", "linux", "amd64", tarball(t, "maiku-desktop-linux-amd64", "new", tar.TypeReg), ""},
		{"desktop", "windows", "amd64", []byte("new"), ""},
		{"desktop", "darwin", "arm64", zipball(t, map[string]string{"maiku.app/Contents/MacOS/maiku-desktop": "new", "maiku.app/Contents/Info.plist": "plist"}), "Contents/MacOS/maiku-desktop"},
	} {
		t.Run(tc.product+"-"+tc.goos, func(t *testing.T) {
			c, u, _ := fixture(t, tc.product, tc.goos, tc.arch, tc.data)
			target := filepath.Join(t.TempDir(), "installed")
			readPath := target
			if tc.inside != "" {
				readPath = filepath.Join(target, filepath.FromSlash(tc.inside))
				if err := os.MkdirAll(filepath.Dir(readPath), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(readPath, []byte("old"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := c.install(context.Background(), u, target); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(readPath)
			if err != nil || string(got) != "new" {
				t.Fatalf("%q %v", got, err)
			}
			if err := c.install(context.Background(), u, target); err == nil {
				t.Fatal("allowed duplicate install from old process")
			}
		})
	}
}

func TestBadArchiveLeavesInstallationUntouched(t *testing.T) {
	for _, data := range [][]byte{
		tarball(t, "../../outside", "evil", tar.TypeReg),
		tarball(t, "maiku", "", tar.TypeSymlink),
		tarball(t, "wrong-binary", "new", tar.TypeReg),
		tarball(t, "maiku", "", tar.TypeReg),
		[]byte("not an archive"),
	} {
		c, u, _ := fixture(t, "cli", "linux", "amd64", data)
		target := filepath.Join(t.TempDir(), "maiku")
		os.WriteFile(target, []byte("old"), 0755)
		if err := c.install(context.Background(), u, target); err == nil {
			t.Fatal("accepted bad archive")
		}
		got, _ := os.ReadFile(target)
		if string(got) != "old" {
			t.Fatal("damaged installation")
		}
		unlock, err := acquireLock(target + ".update-lock")
		if err != nil {
			t.Fatal("left lock held after failure:", err)
		}
		unlock()
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestChecksumFailure(t *testing.T) {
	c, u, _ := fixture(t, "cli", "linux", "amd64", tarball(t, "maiku", "new", tar.TypeReg))
	// Keep the expected hash but alter the downloaded bytes.
	original := c.http.Transport
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("corrupted")), Header: make(http.Header)}, nil
		}
		return original.RoundTrip(r)
	})}
	target := filepath.Join(t.TempDir(), "maiku")
	os.WriteFile(target, []byte("old"), 0755)
	if err := c.install(context.Background(), u, target); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Fatal("damaged original")
	}
	for _, value := range []string{"", "bad  file", strings.Repeat("a", 64) + " file\n" + strings.Repeat("b", 64) + " file"} {
		if _, err := checksum(value, "file"); err == nil {
			t.Fatal("accepted invalid checksum")
		}
	}
}

func TestInstallationLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	unlock, err := acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := acquireLock(path); err == nil {
		second()
		t.Fatal("allowed concurrent update")
	}
	unlock()
	unlock, err = acquireLock(path)
	if err != nil {
		t.Fatal("did not release lock:", err)
	}
	unlock()
}

func TestZipRejectsTraversalAndSymlinks(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	h := &zip.FileHeader{Name: "maiku.exe"}
	h.SetMode(os.ModeSymlink | 0755)
	w, _ := z.CreateHeader(h)
	w.Write([]byte("../../escape"))
	z.Close()
	for _, data := range [][]byte{b.Bytes(), zipball(t, map[string]string{"../escape": "evil"})} {
		stage := t.TempDir()
		archive := filepath.Join(stage, "download")
		os.WriteFile(archive, data, 0600)
		if _, err := unpack(archive, stage, "maiku-cli-windows-amd64.zip", "cli", "windows"); err == nil {
			t.Fatal("accepted unsafe zip")
		}
	}
}

func TestDownloadFailures(t *testing.T) {
	for _, status := range []int{403, 404, 429, 500} {
		c := New("cli")
		c.http = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("failure")), Header: make(http.Header)}, nil
		})}
		if err := c.get(context.Background(), "https://example.invalid", io.Discard, 10); err == nil {
			t.Fatalf("accepted HTTP %d", status)
		}
	}
	c := New("cli")
	c.http = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("oversized")), Header: make(http.Header)}, nil
	})}
	if err := c.get(context.Background(), "https://example.invalid", io.Discard, 2); err == nil {
		t.Fatal("ignored size limit")
	}
	production := New("cli")
	req, _ := http.NewRequest(http.MethodGet, "http://example.invalid", nil)
	if err := production.http.CheckRedirect(req, nil); err == nil {
		t.Fatal("accepted insecure redirect")
	}
}

func TestReplaceRollback(t *testing.T) {
	dir := t.TempDir()
	target, backup := filepath.Join(dir, "target"), filepath.Join(dir, "backup")
	os.WriteFile(target, []byte("old"), 0755)
	keep, err := replaceInstallation(target, filepath.Join(dir, "missing"), backup, os.Rename)
	if err == nil || keep {
		t.Fatalf("keep=%v err=%v", keep, err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Fatal("rollback failed")
	}
	count := 0
	keep, err = replaceInstallation(target, "missing", backup, func(from, to string) error {
		count++
		if count > 1 {
			return errors.New("simulated failure")
		}
		return os.Rename(from, to)
	})
	if !keep || err == nil || !strings.Contains(err.Error(), backup) {
		t.Fatalf("backup lost: %v %v", keep, err)
	}
	if got, _ := os.ReadFile(backup); string(got) != "old" {
		t.Fatal("backup not retained")
	}
}

func TestPathsAndCancellation(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "C:/windows", "a\\b", "a/../../escape", ""} {
		if _, err := archivePath(t.TempDir(), name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	bundle := filepath.Join(t.TempDir(), "maiku.app")
	exe := filepath.Join(bundle, "Contents", "MacOS", "maiku-desktop")
	if got, err := installTarget(exe, "desktop", "darwin"); got != bundle || err != nil {
		t.Fatalf("%s %v", got, err)
	}
	if _, err := installTarget("/tmp/maiku", "desktop", "darwin"); err == nil {
		t.Fatal("accepted non-bundle")
	}
	c, _, _ := fixture(t, "cli", "linux", "amd64", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Check(ctx); err == nil {
		t.Fatal("ignored cancellation")
	}
}

// Runs on each native CI runner: a copied test executable replaces itself while
// running, exercising real OS file locking rather than only synthetic files.
func TestRunningExecutableReplacement(t *testing.T) {
	if os.Getenv("MAIKU_UPDATER_TEST_HELPER") == "1" {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		stage := filepath.Join(filepath.Dir(exe), "replacement")
		os.WriteFile(stage, []byte("replacement"), 0755)
		if _, err := replaceInstallation(exe, stage, exe+".previous", os.Rename); err != nil {
			t.Fatal(err)
		}
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "running")
	if runtime.GOOS == "windows" {
		dest += ".exe"
	}
	if err := os.WriteFile(dest, data, 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, dest, "-test.run=^TestRunningExecutableReplacement$")
	cmd.Env = append(os.Environ(), "MAIKU_UPDATER_TEST_HELPER=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running replacement: %v\n%s", err, out)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "replacement" {
		t.Fatal("running executable was not replaced")
	}
}
