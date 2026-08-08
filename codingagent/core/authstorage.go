package core

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/mikus/maiku/ai/auth"
	"github.com/mikus/maiku/codingagent"
)

// Credential types stored in auth.json.
const (
	CredentialAPIKey = "api_key"
	CredentialOAuth  = "oauth"
)

// Credential is one provider entry in auth.json. It is a flattened union of
// the TypeScript ApiKeyCredential and OAuthCredential shapes; Extra preserves
// any additional fields so a rewrite does not drop data written by the
// TypeScript CLI.
type Credential struct {
	Type string `json:"type"`

	// API key credentials.
	Key string            `json:"key,omitempty"`
	Env map[string]string `json:"env,omitempty"`

	// OAuth credentials.
	Refresh string `json:"refresh,omitempty"`
	Access  string `json:"access,omitempty"`
	Expires int64  `json:"expires,omitempty"`

	// Extra holds fields this port does not model.
	Extra map[string]json.RawMessage `json:"-"`
}

var credentialKnownFields = map[string]bool{
	"type": true, "key": true, "env": true, "refresh": true, "access": true, "expires": true,
}

// UnmarshalJSON keeps unmodelled fields in Extra.
func (c *Credential) UnmarshalJSON(data []byte) error {
	type alias Credential
	var known alias
	if err := json.Unmarshal(data, &known); err != nil {
		return err
	}
	*c = Credential(known)

	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	for key, value := range all {
		if credentialKnownFields[key] {
			continue
		}
		if c.Extra == nil {
			c.Extra = map[string]json.RawMessage{}
		}
		c.Extra[key] = value
	}
	return nil
}

// MarshalJSON writes the modelled fields plus everything kept in Extra.
func (c Credential) MarshalJSON() ([]byte, error) {
	type alias Credential
	data, err := json.Marshal(alias(c))
	if err != nil {
		return nil, err
	}
	if len(c.Extra) == 0 {
		return data, nil
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(data, &merged); err != nil {
		return nil, err
	}
	for key, value := range c.Extra {
		if _, exists := merged[key]; !exists {
			merged[key] = value
		}
	}
	return json.Marshal(merged)
}

// AuthStorage reads and writes the provider credential file (auth.json).
// The file is written with mode 0600 and its parent directory with 0700.
type AuthStorage struct {
	path string

	mu   sync.Mutex
	data map[string]Credential
}

// NewAuthStorage opens the credential store at path. A missing or unreadable
// file yields an empty store rather than an error; write operations recreate
// it.
func NewAuthStorage(path string) *AuthStorage {
	if path == "" {
		path = codingagent.GetAuthPath()
	}
	storage := &AuthStorage{path: codingagent.ExpandTildePath(path), data: map[string]Credential{}}
	storage.Reload()
	return storage
}

// Path returns the credential file path.
func (s *AuthStorage) Path() string { return s.path }

// Reload re-reads the credential file, keeping the last good snapshot when
// the file is missing or malformed.
func (s *AuthStorage) Reload() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.mu.Lock()
			s.data = map[string]Credential{}
			s.mu.Unlock()
		}
		return
	}
	var parsed map[string]Credential
	if err := json.Unmarshal(data, &parsed); err != nil {
		return
	}
	if parsed == nil {
		parsed = map[string]Credential{}
	}
	s.mu.Lock()
	s.data = parsed
	s.mu.Unlock()
}

// Read returns the stored credential for a provider. API-key credentials have
// their key resolved through ResolveConfigValue, so `!command` and `$VAR`
// forms work the same as in the TypeScript store.
func (s *AuthStorage) Read(provider string) (Credential, bool) {
	s.mu.Lock()
	credential, ok := s.data[provider]
	s.mu.Unlock()
	if !ok {
		return Credential{}, false
	}
	if credential.Type == CredentialAPIKey && credential.Key != "" {
		credential.Key = ResolveConfigValue(credential.Key, credential.Env)
	}
	return credential, true
}

// List returns the provider ids and credential types without resolving or
// exposing secrets.
func (s *AuthStorage) List() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.data))
	for provider, credential := range s.data {
		out[provider] = credential.Type
	}
	return out
}

// Write stores a credential for a provider and persists the file.
func (s *AuthStorage) Write(provider string, credential Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = map[string]Credential{}
	}
	s.data[provider] = credential
	return s.persistLocked()
}

// Delete removes a provider's credential and persists the file.
func (s *AuthStorage) Delete(provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, provider)
	return s.persistLocked()
}

func (s *AuthStorage) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(s.path, encoded, 0o600); err != nil {
		return err
	}
	// WriteFile only applies the mode when creating the file, so an existing
	// file with looser permissions is tightened explicitly.
	return os.Chmod(s.path, 0o600)
}

// APIKey returns a usable API key for a provider from the store, or "" when
// there is none. OAuth credentials expose their access token.
func (s *AuthStorage) APIKey(provider string) string {
	credential, ok := s.Read(provider)
	if !ok {
		return ""
	}
	switch credential.Type {
	case CredentialAPIKey:
		return credential.Key
	case CredentialOAuth:
		return credential.Access
	}
	return ""
}

var defaultAuthStorage struct {
	once    sync.Once
	storage *AuthStorage
}

// DefaultAuthStorage returns the process-wide store for ~/.maiku/agent/auth.json.
func DefaultAuthStorage() *AuthStorage {
	defaultAuthStorage.once.Do(func() {
		defaultAuthStorage.storage = NewAuthStorage(codingagent.GetAuthPath())
	})
	return defaultAuthStorage.storage
}

// InstallAuthStorage makes auth.ResolveAPIKey fall back to auth.json after
// the provider environment variables.
func InstallAuthStorage(storage *AuthStorage) {
	if storage == nil {
		auth.SetCredentialLookup(nil)
		return
	}
	auth.SetCredentialLookup(storage.APIKey)
}

var (
	envVarNameRe       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	envVarNamePrefixRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*`)

	configCommandMu    sync.Mutex
	configCommandCache = map[string]string{}
)

// ResolveConfigValue resolves a stored config value. A leading "!" runs the
// rest as a shell command and uses its trimmed stdout; otherwise `$VAR` and
// `${VAR}` are expanded from overrides then the process environment, with
// `$$` and `$!` as escapes.
func ResolveConfigValue(value string, overrides map[string]string) string {
	if strings.HasPrefix(value, "!") {
		return resolveCommandConfigValue(strings.TrimPrefix(value, "!"))
	}
	return expandConfigTemplate(value, overrides)
}

// IsCommandConfigValue reports whether a config value runs a shell command.
func IsCommandConfigValue(value string) bool {
	return strings.HasPrefix(value, "!")
}

func resolveCommandConfigValue(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}

	configCommandMu.Lock()
	defer configCommandMu.Unlock()
	if cached, ok := configCommandCache[command]; ok {
		return cached
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	output, err := exec.Command(shell, "-c", command).Output()
	result := ""
	if err == nil {
		result = strings.TrimSpace(string(output))
	}
	configCommandCache[command] = result
	return result
}

func expandConfigTemplate(value string, overrides map[string]string) string {
	var out strings.Builder
	for index := 0; index < len(value); {
		dollar := strings.IndexByte(value[index:], '$')
		if dollar < 0 {
			out.WriteString(value[index:])
			break
		}
		out.WriteString(value[index : index+dollar])
		index += dollar

		if index+1 >= len(value) {
			out.WriteByte('$')
			index++
			continue
		}

		switch next := value[index+1]; {
		case next == '$' || next == '!':
			out.WriteByte(next)
			index += 2

		case next == '{':
			end := strings.IndexByte(value[index+2:], '}')
			if end < 0 {
				out.WriteByte('$')
				index++
				continue
			}
			end += index + 2
			name := value[index+2 : end]
			if envVarNameRe.MatchString(name) {
				out.WriteString(lookupConfigEnv(name, overrides))
			} else {
				out.WriteString(value[index : end+1])
			}
			index = end + 1

		default:
			name := envVarNamePrefixRe.FindString(value[index+1:])
			if name == "" {
				out.WriteByte('$')
				index++
				continue
			}
			out.WriteString(lookupConfigEnv(name, overrides))
			index += 1 + len(name)
		}
	}
	return out.String()
}

func lookupConfigEnv(name string, overrides map[string]string) string {
	if value := overrides[name]; value != "" {
		return value
	}
	return os.Getenv(name)
}
