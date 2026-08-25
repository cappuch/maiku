// Package mcp loads Cursor-compatible mcp.json configs and runs stdio MCP
// servers as tool providers for the maiku coding agent.
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/mikus/maiku/codingagent"
)

// File is the on-disk shape of mcp.json (Cursor / Claude Desktop compatible).
type File struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// ServerConfig describes a single stdio MCP server.
type ServerConfig struct {
	// Command is the executable to launch (required for stdio).
	Command string `json:"command"`
	// Args are passed to Command.
	Args []string `json:"args,omitempty"`
	// Env are extra environment variables for the server process.
	Env map[string]string `json:"env,omitempty"`
	// Type is optional; "stdio" (or empty) is supported. Other types are ignored.
	Type string `json:"type,omitempty"`
	// Disabled skips connecting when true.
	Disabled bool `json:"disabled,omitempty"`
}

// IsStdio reports whether this entry should be launched as a local stdio server.
func (c ServerConfig) IsStdio() bool {
	t := strings.ToLower(strings.TrimSpace(c.Type))
	return t == "" || t == "stdio"
}

// Enabled reports whether the server should be connected.
func (c ServerConfig) Enabled() bool {
	return !c.Disabled && c.IsStdio() && strings.TrimSpace(c.Command) != ""
}

// GlobalPath returns ~/.maiku/agent/mcp.json.
func GlobalPath(agentDir string) string {
	if agentDir == "" {
		agentDir = codingagent.GetAgentDir()
	}
	return filepath.Join(codingagent.ExpandTildePath(agentDir), "mcp.json")
}

// ProjectPath returns <cwd>/.maiku/mcp.json.
func ProjectPath(cwd string) string {
	return filepath.Join(codingagent.ExpandTildePath(cwd), codingagent.CONFIG_DIR_NAME, "mcp.json")
}

// LoadResult is the merged view of global and project MCP configs.
type LoadResult struct {
	Servers    map[string]ServerConfig
	GlobalPath string
	ProjectPath string
	Errors     []error
}

// Load reads and merges global and project mcp.json files. Project entries
// override global entries with the same name. Missing files are not errors.
func Load(cwd, agentDir string) LoadResult {
	result := LoadResult{
		Servers:     map[string]ServerConfig{},
		GlobalPath:  GlobalPath(agentDir),
		ProjectPath: ProjectPath(cwd),
	}

	global, err := readFile(result.GlobalPath)
	if err != nil {
		result.Errors = append(result.Errors, err)
	} else {
		maps.Copy(result.Servers, global.MCPServers)
	}

	if strings.TrimSpace(cwd) != "" {
		project, err := readFile(result.ProjectPath)
		if err != nil {
			result.Errors = append(result.Errors, err)
		} else {
			maps.Copy(result.Servers, project.MCPServers)
		}
	}
	return result
}

func readFile(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return File{MCPServers: map[string]ServerConfig{}}, nil
		}
		return File{}, fmt.Errorf("read mcp config %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return File{MCPServers: map[string]ServerConfig{}}, nil
	}
	var file File
	if err := json.Unmarshal(data, &file); err != nil {
		return File{}, fmt.Errorf("parse mcp config %s: %w", path, err)
	}
	if file.MCPServers == nil {
		file.MCPServers = map[string]ServerConfig{}
	}
	return file, nil
}

// UpsertGlobal writes or updates a single server entry in the global mcp.json.
func UpsertGlobal(agentDir, name string, cfg ServerConfig) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("server name is required")
	}
	if strings.TrimSpace(cfg.Command) == "" {
		return errors.New("command is required")
	}
	path := GlobalPath(agentDir)
	file, err := readFile(path)
	if err != nil {
		return err
	}
	if file.MCPServers == nil {
		file.MCPServers = map[string]ServerConfig{}
	}
	file.MCPServers[name] = cfg
	return writeFile(path, file)
}

// RemoveGlobal deletes a server entry from the global mcp.json.
func RemoveGlobal(agentDir, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("server name is required")
	}
	path := GlobalPath(agentDir)
	file, err := readFile(path)
	if err != nil {
		return err
	}
	if file.MCPServers == nil {
		return nil
	}
	if _, ok := file.MCPServers[name]; !ok {
		return fmt.Errorf("unknown MCP server %q", name)
	}
	delete(file.MCPServers, name)
	return writeFile(path, file)
}

// SetDisabledGlobal toggles the disabled flag for a global server entry.
func SetDisabledGlobal(agentDir, name string, disabled bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("server name is required")
	}
	path := GlobalPath(agentDir)
	file, err := readFile(path)
	if err != nil {
		return err
	}
	cfg, ok := file.MCPServers[name]
	if !ok {
		return fmt.Errorf("unknown MCP server %q", name)
	}
	cfg.Disabled = disabled
	file.MCPServers[name] = cfg
	return writeFile(path, file)
}

func writeFile(path string, file File) error {
	if file.MCPServers == nil {
		file.MCPServers = map[string]ServerConfig{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
