package core

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mikus/maiku/ai"
	"github.com/mikus/maiku/codingagent"
)

// SessionHeader is the first line of a session JSONL file.
type SessionHeader struct {
	Type      string `json:"type"` // "session"
	Version   int    `json:"version"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
}

// SessionEntry is a non-header line of a session JSONL file.
//
// This is a simplification of the TypeScript session format: entries still
// carry id/parentId so files stay readable by the tree-aware implementation,
// but this port only ever appends a linear chain and only handles "message"
// entries.
type SessionEntry struct {
	Type      string          `json:"type"` // "message"
	ID        string          `json:"id"`
	ParentID  *string         `json:"parentId"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message,omitempty"`
}

// SessionManager persists a conversation as newline-delimited JSON under the
// sessions directory and can reload a previous one.
type SessionManager struct {
	mu sync.Mutex

	dir     string
	cwd     string
	persist bool

	file     string
	header   SessionHeader
	leafID   string
	messages []ai.Message

	headerWritten bool
}

func nowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// NewSessionManager starts a fresh session. When persist is false nothing is
// written to disk and the session is purely in-memory.
func NewSessionManager(cwd, dir string, persist bool) *SessionManager {
	timestamp := nowISO()
	id := ai.UUIDv7()

	sm := &SessionManager{
		dir:     dir,
		cwd:     cwd,
		persist: persist,
		header: SessionHeader{
			Type:      "session",
			Version:   codingagent.SessionVersion,
			ID:        id,
			Timestamp: timestamp,
			Cwd:       cwd,
		},
	}

	if persist {
		fileTimestamp := strings.NewReplacer(":", "-", ".", "-").Replace(timestamp)
		sm.file = filepath.Join(dir, fmt.Sprintf("%s_%s.jsonl", fileTimestamp, id))
	}

	return sm
}

// LoadSessionManager reopens an existing session file for appending.
func LoadSessionManager(path string) (*SessionManager, error) {
	header, entries, err := readSessionFile(path)
	if err != nil {
		return nil, err
	}

	sm := &SessionManager{
		dir:           filepath.Dir(path),
		cwd:           header.Cwd,
		persist:       true,
		file:          path,
		header:        header,
		headerWritten: true,
	}

	for _, entry := range entries {
		if entry.Type != "message" || len(entry.Message) == 0 {
			continue
		}
		message, err := DecodeMessage(entry.Message)
		if err != nil {
			continue // tolerate hand-edited or future-format lines
		}
		switch message.Role {
		case "user", "assistant", "toolResult":
			sm.messages = append(sm.messages, message)
		}
		sm.leafID = entry.ID
	}

	return sm, nil
}

// ResolveSessionPath maps a --session value (absolute/relative file path, or
// a session id prefix) to a session file under one of the search directories.
func ResolveSessionPath(dirs []string, value string) (string, error) {
	if strings.ContainsAny(value, string(os.PathSeparator)) || strings.HasSuffix(value, ".jsonl") {
		path := codingagent.ExpandTildePath(value)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		files, err := sessionFilesNewestFirst(dir)
		if err != nil {
			continue
		}
		for _, path := range files {
			header, err := readSessionHeader(path)
			if err != nil {
				continue
			}
			if strings.HasPrefix(header.ID, value) || strings.Contains(filepath.Base(path), value) {
				return path, nil
			}
		}
	}

	return "", fmt.Errorf("no session found matching %q", value)
}

// FindMostRecentSession returns the newest session file recorded for cwd, or
// "" when there is none.
func FindMostRecentSession(dir, cwd string) string {
	files, err := sessionFilesNewestFirst(dir)
	if err != nil {
		return ""
	}
	for _, path := range files {
		header, err := readSessionHeader(path)
		if err != nil {
			continue
		}
		if cwd == "" || sameDir(header.Cwd, cwd) {
			return path
		}
	}
	return ""
}

func sameDir(a, b string) bool {
	if a == b {
		return true
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	return errA == nil && errB == nil && absA == absB
}

func sessionFilesNewestFirst(dir string) ([]string, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	type fileInfo struct {
		path    string
		modTime time.Time
	}
	var files []fileInfo
	for _, e := range dirEntries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{path: filepath.Join(dir, e.Name()), modTime: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })

	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.path)
	}
	return paths, nil
}

func readSessionFile(path string) (SessionHeader, []SessionEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return SessionHeader{}, nil, err
	}
	defer file.Close()

	var header SessionHeader
	var entries []SessionEntry

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			continue
		}
		if probe.Type == "session" {
			if err := json.Unmarshal([]byte(line), &header); err != nil {
				return SessionHeader{}, nil, err
			}
			continue
		}
		var entry SessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return SessionHeader{}, nil, err
	}
	if header.ID == "" {
		return SessionHeader{}, nil, fmt.Errorf("not a session file: %s", path)
	}

	return header, entries, nil
}

// PruneEmptySessions deletes session JSONL files under dir (and one level of
// cwd-scoped subdirectories, matching ListSessionSummaries) that contain no
// message entries. Paths in keep are never touched (e.g. live sessions). It
// returns the number of files removed.
func PruneEmptySessions(dir string, keep map[string]bool) int {
	removed := 0
	var walk func(d string, recurse bool)
	walk = func(d string, recurse bool) {
		entries, err := os.ReadDir(d)
		if err != nil {
			return
		}
		for _, e := range entries {
			full := filepath.Join(d, e.Name())
			if e.IsDir() {
				if recurse {
					walk(full, false)
				}
				continue
			}
			if !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			if keep[full] {
				continue
			}
			if !sessionIsEmpty(full) {
				continue
			}
			if err := os.Remove(full); err == nil {
				removed++
			}
		}
	}
	walk(dir, true)
	return removed
}

// sessionIsEmpty reports whether path is a session file with a valid header
// but no message entries. Scanning stops at the first message so populated
// sessions are cheap to skip.
func sessionIsEmpty(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	hasHeader := false
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			continue
		}
		switch probe.Type {
		case "session":
			var header SessionHeader
			if err := json.Unmarshal([]byte(line), &header); err == nil && header.ID != "" {
				hasHeader = true
			}
		case "message":
			return false
		}
	}
	return hasHeader
}

func readSessionHeader(path string) (SessionHeader, error) {
	file, err := os.Open(path)
	if err != nil {
		return SessionHeader{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var header SessionHeader
		if err := json.Unmarshal([]byte(line), &header); err != nil {
			return SessionHeader{}, err
		}
		if header.Type != "session" || header.ID == "" {
			return SessionHeader{}, fmt.Errorf("not a session file: %s", path)
		}
		return header, nil
	}
	return SessionHeader{}, fmt.Errorf("empty session file: %s", path)
}

// Header returns the session header.
func (s *SessionManager) Header() SessionHeader {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.header
}

// File returns the session file path, or "" for ephemeral sessions.
func (s *SessionManager) File() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file
}

// Messages returns the loaded transcript.
func (s *SessionManager) Messages() []ai.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ai.Message{}, s.messages...)
}

// EnsurePersisted writes the session header to disk if this manager is
// configured to persist and the file does not exist yet. Empty new sessions
// otherwise stay invisible to ListSessionSummaries until the first message.
func (s *SessionManager) EnsurePersisted() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.persist || s.file == "" || s.headerWritten {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.file), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(s.file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	headerLine, err := json.Marshal(s.header)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(headerLine, '\n')); err != nil {
		return err
	}
	s.headerWritten = true
	return nil
}

// AppendMessage records a message in memory and, for persisted sessions,
// appends it to the session file.
func (s *SessionManager) AppendMessage(message ai.Message) error {
	encoded, err := EncodeMessage(message)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = append(s.messages, message)

	entry := SessionEntry{
		Type:      "message",
		ID:        ai.UUIDv7(),
		Timestamp: nowISO(),
		Message:   encoded,
	}
	if s.leafID != "" {
		parent := s.leafID
		entry.ParentID = &parent
	}
	s.leafID = entry.ID

	if !s.persist || s.file == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(s.file), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(s.file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	if !s.headerWritten {
		headerLine, err := json.Marshal(s.header)
		if err != nil {
			return err
		}
		if _, err := file.Write(append(headerLine, '\n')); err != nil {
			return err
		}
		s.headerWritten = true
	}

	entryLine, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = file.Write(append(entryLine, '\n'))
	return err
}
