// Package codingagent holds shared configuration for the maiku coding agent:
// app identity, version, and the on-disk layout of the agent directory.
package codingagent

import (
	"os"
	"path/filepath"
	"strings"
)

// Identity constants.
const (
	// VERSION is the coding agent version reported by --version.
	VERSION = "0.1.0"
	// APP_NAME is the binary/command name.
	APP_NAME = "maiku"
	// APP_TITLE is the display title used in banners.
	APP_TITLE = "maiku"
	// CONFIG_DIR_NAME is the per-user config directory under $HOME.
	CONFIG_DIR_NAME = ".maiku"
	// SessionVersion is the version stamped into new session headers.
	SessionVersion = 3
)

// Environment variables that override the default directory layout.
var (
	ENV_AGENT_DIR   = "MAIKU_AGENT_DIR"
	ENV_SESSION_DIR = "MAIKU_SESSION_DIR"
)

// ExpandTildePath resolves a leading ~ to the user's home directory and
// returns an absolute path when possible.
func ExpandTildePath(path string) string {
	if path == "" {
		return path
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// GetAgentDir returns the agent config directory (default ~/.maiku/agent).
func GetAgentDir() string {
	if envDir := os.Getenv(ENV_AGENT_DIR); envDir != "" {
		return ExpandTildePath(envDir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", CONFIG_DIR_NAME, "agent")
	}
	return filepath.Join(home, CONFIG_DIR_NAME, "agent")
}

// GetSessionsDir returns the root directory holding session JSONL files.
func GetSessionsDir() string {
	if envDir := os.Getenv(ENV_SESSION_DIR); envDir != "" {
		return ExpandTildePath(envDir)
	}
	return filepath.Join(GetAgentDir(), "sessions")
}

// GetDefaultSessionDir returns the cwd-scoped session directory:
// ~/.maiku/agent/sessions/--{encoded-cwd}--/
func GetDefaultSessionDir(cwd string) string {
	root := GetSessionsDir()
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	trimmed := strings.TrimLeft(abs, `/\`)
	safe := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':':
			return '-'
		default:
			return r
		}
	}, trimmed)
	return filepath.Join(root, "--"+safe+"--")
}

// GetSettingsPath returns the path to settings.json.
func GetSettingsPath() string {
	return filepath.Join(GetAgentDir(), "settings.json")
}

// GetAuthPath returns the path to auth.json.
func GetAuthPath() string {
	return filepath.Join(GetAgentDir(), "auth.json")
}
