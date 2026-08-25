package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
	"github.com/mikus/maiku/codingagent"
)

const toolPrefix = "mcp__"

// Status is a snapshot of connected MCP servers for the UI.
type Status struct {
	Configured int             `json:"configured"`
	Connected  int             `json:"connected"`
	Failed     int             `json:"failed"`
	Servers    []ServerStatus  `json:"servers"`
}

// ServerStatus is the runtime view of one configured MCP server.
type ServerStatus struct {
	Name      string            `json:"name"`
	Kind      string            `json:"kind"` // stdio | http | sse
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Disabled  bool              `json:"disabled"`
	Connected bool              `json:"connected"`
	Error     string            `json:"error,omitempty"`
	ToolCount int               `json:"toolCount"`
	Tools     []string          `json:"tools,omitempty"`
	Scope     string            `json:"scope"` // "global" | "project"
}

type liveServer struct {
	name    string
	cfg     ServerConfig
	scope   string
	session *mcp.ClientSession
	tools   []agent.AgentTool
	err     string
}

// Manager owns MCP client sessions for the desktop/agent process.
type Manager struct {
	mu       sync.Mutex
	client   *mcp.Client
	servers  map[string]*liveServer
	cwd      string
	agentDir string
}

// NewManager creates an idle MCP manager.
func NewManager() *Manager {
	return &Manager{
		client:  mcp.NewClient(&mcp.Implementation{Name: "maiku", Version: codingagent.VERSION}, nil),
		servers: map[string]*liveServer{},
	}
}

// Close disconnects every MCP server.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, live := range m.servers {
		if live.session != nil {
			_ = live.session.Close()
		}
		delete(m.servers, name)
	}
}

// Sync loads mcp.json for cwd, (re)connects enabled servers, and drops removed ones.
func (m *Manager) Sync(ctx context.Context, cwd, agentDir string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if agentDir == "" {
		agentDir = codingagent.GetAgentDir()
	}

	loaded := Load(cwd, agentDir)

	m.mu.Lock()
	m.cwd = cwd
	m.agentDir = agentDir
	desired := mapsClone(loaded.Servers)
	// Drop servers no longer in config.
	for name, live := range m.servers {
		if _, ok := desired[name]; !ok {
			if live.session != nil {
				_ = live.session.Close()
			}
			delete(m.servers, name)
		}
	}
	m.mu.Unlock()

	var firstErr error
	names := make([]string, 0, len(desired))
	for name := range desired {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		cfg := desired[name]
		scope := serverScope(name, loaded)
		if err := m.syncOne(ctx, name, cfg, scope); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func serverScope(name string, loaded LoadResult) string {
	if p, err := readFile(loaded.ProjectPath); err == nil {
		if _, ok := p.MCPServers[name]; ok {
			return "project"
		}
	}
	return "global"
}

func mapsClone(in map[string]ServerConfig) map[string]ServerConfig {
	out := make(map[string]ServerConfig, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (m *Manager) syncOne(ctx context.Context, name string, cfg ServerConfig, scope string) error {
	m.mu.Lock()
	existing := m.servers[name]
	if existing != nil && configsEqual(existing.cfg, cfg) {
		healthy := existing.session != nil && existing.err == "" && cfg.Enabled()
		idle := !cfg.Enabled() && existing.session == nil
		if healthy || idle {
			existing.scope = scope
			m.mu.Unlock()
			return nil
		}
	}
	if existing != nil && existing.session != nil {
		_ = existing.session.Close()
	}
	delete(m.servers, name)
	m.mu.Unlock()

	live := &liveServer{name: name, cfg: cfg, scope: scope}
	if !cfg.Enabled() {
		m.mu.Lock()
		m.servers[name] = live
		m.mu.Unlock()
		return nil
	}

	session, tools, err := m.connect(ctx, name, cfg)
	if err != nil {
		live.err = err.Error()
		m.mu.Lock()
		m.servers[name] = live
		m.mu.Unlock()
		return err
	}
	live.session = session
	live.tools = tools
	m.mu.Lock()
	m.servers[name] = live
	m.mu.Unlock()
	return nil
}

func configsEqual(a, b ServerConfig) bool {
	if a.Command != b.Command || a.Disabled != b.Disabled || a.Type != b.Type || a.URL != b.URL {
		return false
	}
	if a.Kind() != b.Kind() {
		return false
	}
	if len(a.Args) != len(b.Args) {
		return false
	}
	for i := range a.Args {
		if a.Args[i] != b.Args[i] {
			return false
		}
	}
	if !stringMapsEqual(a.Env, b.Env) || !stringMapsEqual(a.Headers, b.Headers) {
		return false
	}
	return true
}

func stringMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func (m *Manager) connect(ctx context.Context, name string, cfg ServerConfig) (*mcp.ClientSession, []agent.AgentTool, error) {
	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	transport, err := buildTransport(cfg, m.cwd)
	if err != nil {
		return nil, nil, fmt.Errorf("connect %s: %w", name, err)
	}
	session, err := m.client.Connect(connectCtx, transport, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("connect %s: %w", name, err)
	}

	var tools []agent.AgentTool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			_ = session.Close()
			return nil, nil, fmt.Errorf("list tools %s: %w", name, err)
		}
		agentTool, err := bridgeTool(name, session, tool)
		if err != nil {
			_ = session.Close()
			return nil, nil, err
		}
		tools = append(tools, agentTool)
	}
	return session, tools, nil
}

func buildTransport(cfg ServerConfig, cwd string) (mcp.Transport, error) {
	switch cfg.Kind() {
	case "stdio":
		cmd := exec.Command(cfg.Command, cfg.Args...)
		cmd.Env = mergeEnv(os.Environ(), cfg.Env)
		if cwd != "" {
			cmd.Dir = cwd
		}
		return &mcp.CommandTransport{Command: cmd}, nil
	case "sse":
		return &mcp.SSEClientTransport{
			Endpoint:   cfg.URL,
			HTTPClient: httpClientWithHeaders(cfg.Headers),
		}, nil
	case "http":
		return &mcp.StreamableClientTransport{
			Endpoint:   cfg.URL,
			HTTPClient: httpClientWithHeaders(cfg.Headers),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported transport %q", cfg.Type)
	}
}

type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (h headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	for k, v := range h.headers {
		r.Header.Set(k, expandEnvValue(v))
	}
	base := h.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

func httpClientWithHeaders(headers map[string]string) *http.Client {
	if len(headers) == 0 {
		return nil
	}
	return &http.Client{
		Transport: headerRoundTripper{
			base:    http.DefaultTransport,
			headers: mapsCloneString(headers),
		},
	}
}

func mergeEnv(base []string, extra map[string]string) []string {
	if len(extra) == 0 {
		return base
	}
	out := append([]string{}, base...)
	for k, v := range extra {
		out = append(out, k+"="+expandEnvValue(v))
	}
	return out
}

// expandEnvValue resolves ${VAR} and $VAR references against the process env.
func expandEnvValue(v string) string {
	return os.Expand(v, os.Getenv)
}

// Tools returns AgentTools for every connected MCP server.
func (m *Manager) Tools() []agent.AgentTool {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []agent.AgentTool
	for _, name := range names {
		live := m.servers[name]
		if live == nil || live.session == nil {
			continue
		}
		out = append(out, live.tools...)
	}
	return out
}

// ToolSnippets returns name→description entries for the system prompt.
func (m *Manager) ToolSnippets() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for _, live := range m.servers {
		if live == nil {
			continue
		}
		for _, tool := range live.tools {
			desc := tool.Description
			if desc == "" {
				desc = "MCP tool from " + live.name
			}
			out[tool.Name] = desc
		}
	}
	return out
}

// Snapshot returns connection status for the UI and status bar.
func (m *Manager) Snapshot() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	sort.Strings(names)

	status := Status{Configured: len(names)}
	for _, name := range names {
		live := m.servers[name]
		st := ServerStatus{
			Name:     name,
			Kind:     live.cfg.Kind(),
			Command:  live.cfg.Command,
			Args:     append([]string{}, live.cfg.Args...),
			Env:      mapsCloneString(live.cfg.Env),
			URL:      live.cfg.URL,
			Headers:  mapsCloneString(live.cfg.Headers),
			Disabled: live.cfg.Disabled,
			Error:    live.err,
			Scope:    live.scope,
		}
		if live.session != nil {
			st.Connected = true
			st.ToolCount = len(live.tools)
			for _, tool := range live.tools {
				// Strip prefix for display of raw tool names where useful.
				st.Tools = append(st.Tools, strings.TrimPrefix(tool.Name, toolPrefix+name+"__"))
			}
			status.Connected++
		} else if live.err != "" {
			status.Failed++
		}
		status.Servers = append(status.Servers, st)
	}
	return status
}

func mapsCloneString(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func bridgeTool(serverName string, session *mcp.ClientSession, tool *mcp.Tool) (agent.AgentTool, error) {
	params, err := schemaBytes(tool.InputSchema)
	if err != nil {
		return agent.AgentTool{}, fmt.Errorf("schema for %s/%s: %w", serverName, tool.Name, err)
	}
	qualified := toolPrefix + serverName + "__" + tool.Name
	desc := tool.Description
	if desc == "" {
		desc = fmt.Sprintf("MCP tool %s from server %s", tool.Name, serverName)
	} else {
		desc = fmt.Sprintf("[MCP:%s] %s", serverName, desc)
	}
	rawName := tool.Name
	return agent.AgentTool{
		Tool: ai.Tool{
			Name:        qualified,
			Description: desc,
			Parameters:  params,
		},
		Label: "MCP · " + serverName,
		Execute: func(ctx context.Context, _ string, args map[string]any, _ agent.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
			res, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name:      rawName,
				Arguments: args,
			})
			if err != nil {
				return agent.AgentToolResult{}, fmt.Errorf("mcp %s: %w", qualified, err)
			}
			text := contentText(res.Content)
			if res.IsError {
				if text == "" {
					text = "MCP tool returned an error"
				}
				return agent.AgentToolResult{}, fmt.Errorf("%s", text)
			}
			if text == "" && res.StructuredContent != nil {
				encoded, encErr := json.Marshal(res.StructuredContent)
				if encErr != nil {
					return agent.AgentToolResult{}, encErr
				}
				text = string(encoded)
			}
			if text == "" {
				text = "(empty MCP tool result)"
			}
			return agent.AgentToolResult{
				Content: []ai.ToolResultContent{{Type: "text", Text: text}},
				Details: map[string]any{
					"mcpServer": serverName,
					"mcpTool":   rawName,
				},
			}, nil
		},
	}, nil
}

func schemaBytes(schema any) (json.RawMessage, error) {
	if schema == nil {
		return json.RawMessage(`{"type":"object","properties":{}}`), nil
	}
	if raw, ok := schema.(json.RawMessage); ok {
		if len(raw) == 0 {
			return json.RawMessage(`{"type":"object","properties":{}}`), nil
		}
		return raw, nil
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	if string(data) == "null" || len(data) == 0 {
		return json.RawMessage(`{"type":"object","properties":{}}`), nil
	}
	return data, nil
}

func contentText(contents []mcp.Content) string {
	var parts []string
	for _, c := range contents {
		switch v := c.(type) {
		case *mcp.TextContent:
			if v != nil && v.Text != "" {
				parts = append(parts, v.Text)
			}
		case *mcp.ImageContent:
			if v != nil {
				parts = append(parts, fmt.Sprintf("[image %s]", v.MIMEType))
			}
		case *mcp.AudioContent:
			if v != nil {
				parts = append(parts, fmt.Sprintf("[audio %s]", v.MIMEType))
			}
		default:
			if c != nil {
				if data, err := json.Marshal(c); err == nil {
					parts = append(parts, string(data))
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}
