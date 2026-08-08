package main

import (
	"encoding/base64"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/mikus/maiku/codingagent/core/tools"
)

// ImageAttachment is a base64-encoded image sent with a prompt from the UI.
type ImageAttachment struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
	Name     string `json:"name,omitempty"`
}

// PathSuggestion is one entry in the @ path autocomplete list.
type PathSuggestion struct {
	Value       string `json:"value"` // insert text including leading @
	Label       string `json:"label"` // display path
	IsDirectory bool   `json:"isDirectory"`
}

// PickedFile is a file chosen via the composer + button.
type PickedFile struct {
	Path     string `json:"path"`    // absolute path
	RelPath  string `json:"relPath"` // path relative to cwd when possible
	Name     string `json:"name"`
	IsImage  bool   `json:"isImage"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"` // base64 when IsImage
}

var skipDirNames = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	".wails":       true,
	".next":        true,
	"target":       true,
	"__pycache__":  true,
	".venv":        true,
	"venv":         true,
}

// CompletePath returns @-mention path suggestions under the current workspace.
func (a *App) CompletePath(query string) []PathSuggestion {
	a.mu.Lock()
	cwd := a.cwd
	a.mu.Unlock()
	if cwd == "" {
		return nil
	}

	query = strings.TrimSpace(query)
	query = strings.TrimPrefix(query, "@")
	if strings.HasPrefix(query, `"`) || strings.HasPrefix(query, `'`) {
		query = query[1:]
	}
	query = strings.ReplaceAll(query, `\`, `/`)

	const maxResults = 40
	lowerQuery := strings.ToLower(query)
	var matches []PathSuggestion

	_ = filepath.WalkDir(cwd, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() && skipDirNames[name] {
			return filepath.SkipDir
		}
		if path == cwd {
			return nil
		}
		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if lowerQuery != "" && !strings.Contains(strings.ToLower(rel), lowerQuery) {
			return nil
		}

		label := rel
		valuePath := rel
		if d.IsDir() {
			label += "/"
			valuePath += "/"
		}
		matches = append(matches, PathSuggestion{
			Value:       "@" + quotePathIfNeeded(valuePath),
			Label:       label,
			IsDirectory: d.IsDir(),
		})
		if len(matches) >= maxResults*3 {
			// Collect a bit extra then rank/truncate.
			return fs.SkipAll
		}
		return nil
	})

	sort.SliceStable(matches, func(i, j int) bool {
		li, lj := strings.ToLower(matches[i].Label), strings.ToLower(matches[j].Label)
		si, sj := strings.HasPrefix(li, lowerQuery), strings.HasPrefix(lj, lowerQuery)
		if si != sj {
			return si
		}
		if matches[i].IsDirectory != matches[j].IsDirectory {
			return !matches[i].IsDirectory
		}
		if len(li) != len(lj) {
			return len(li) < len(lj)
		}
		return li < lj
	})
	if len(matches) > maxResults {
		matches = matches[:maxResults]
	}
	return matches
}

func quotePathIfNeeded(path string) string {
	for _, r := range path {
		if unicode.IsSpace(r) {
			return `"` + path + `"`
		}
	}
	return path
}

// PickFiles opens a native multi-file dialog. Images are returned as base64
// attachments; other files are returned as workspace-relative paths for @ injection.
func (a *App) PickFiles() ([]PickedFile, error) {
	a.mu.Lock()
	cwd := a.cwd
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return nil, nil
	}

	paths, err := runtime.OpenMultipleFilesDialog(ctx, runtime.OpenDialogOptions{
		Title:            "Attach files",
		DefaultDirectory: cwd,
	})
	if err != nil || len(paths) == 0 {
		return nil, err
	}

	out := make([]PickedFile, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		name := filepath.Base(path)
		rel := path
		if cwd != "" {
			if r, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(r, "..") {
				rel = filepath.ToSlash(r)
			}
		}
		pf := PickedFile{Path: path, RelPath: rel, Name: name}
		mime, _ := tools.DetectSupportedImageMimeTypeFromFile(path)
		if mime != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			pf.IsImage = true
			pf.MimeType = mime
			pf.Data = base64.StdEncoding.EncodeToString(data)
		}
		out = append(out, pf)
	}
	return out, nil
}
