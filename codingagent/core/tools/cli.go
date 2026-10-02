package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/cappuch/maiku/agent"
	"github.com/cappuch/maiku/ai"
	"github.com/cappuch/maiku/codingagent/internal/shellcmd"
)

var cliSchema = []byte(`{
 "type":"object",
 "properties":{
  "action":{"type":"string","enum":["start","input","poll","terminate"],"description":"Defaults to start"},
  "command":{"type":"string","description":"Shell command for start"},
  "session_id":{"type":"string","description":"Session returned by start; required for other actions"},
  "input":{"type":"string","description":"Exact stdin text for input; include a newline to submit a line"},
  "close_stdin":{"type":"boolean","description":"Send EOF after input"},
  "yield_ms":{"type":"number","description":"Wait for completion before returning, 0–60000 ms; default 1000"},
  "timeout":{"type":"number","description":"Session lifetime in seconds; default 600"}
 }
}`)

var waitSchema = []byte(`{
 "type":"object",
 "properties":{
  "session_id":{"type":"string","description":"CLI session to wait for; omit to pause"},
  "seconds":{"type":"number","description":"Maximum wait, 0–60 seconds; default 30 for a session, required for a pause"}
 }
}`)

// CLIToolDetails identifies a resumable command and its full output log.
type CLIToolDetails struct {
	SessionID      string
	Running        bool
	ExitCode       *int
	FullOutputPath string
}

// cliSessions belongs to one toolset, so IDs cannot access another agent's commands.
type cliSessions struct {
	mu       sync.Mutex
	next     uint64
	sessions map[string]*cliSession
	cwd      string
	options  BashOptions
}

type cliSession struct {
	mu        sync.Mutex
	inputMu   sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	log       *os.File
	path      string
	pending   string
	truncated bool
	err       error
	exitCode  int
	done      chan struct{}
}

func newCLISessions(cwd string, options BashOptions) *cliSessions {
	return &cliSessions{cwd: cwd, options: options, sessions: make(map[string]*cliSession)}
}

func (s *cliSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.log.Write(p)
	tr := TruncateTail(s.pending+string(p[:n]), nil)
	s.pending = tr.Content
	s.truncated = s.truncated || tr.Truncated
	return n, err
}

func boundedWait(params map[string]any, key string, fallback, scale float64) (time.Duration, error) {
	n := argNumberPtr(params, key)
	v := fallback
	if n != nil {
		v = *n
	}
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v*scale > 60 {
		return 0, fmt.Errorf("%s must be finite and between 0 and %g", key, 60/scale)
	}
	return time.Duration(v * scale * float64(time.Second)), nil
}

func (m *cliSessions) start(params map[string]any) (string, *cliSession, error) {
	command, _ := argString(params, "command")
	if command == "" {
		return "", nil, errors.New("command is required for start")
	}
	timeout := argNumberPtr(params, "timeout")
	if timeout == nil {
		v := float64(600)
		timeout = &v
	}
	lifetime, err := resolveTimeoutMs(timeout)
	if err != nil {
		return "", nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Bound retained sessions, reclaiming completed entries before admitting work.
	if len(m.sessions) >= 16 {
		for id, s := range m.sessions {
			select {
			case <-s.done:
				delete(m.sessions, id)
			default:
			}
			if len(m.sessions) < 16 {
				break
			}
		}
	}
	if len(m.sessions) >= 16 {
		return "", nil, errors.New("too many CLI sessions; terminate a running session first")
	}
	log, err := os.CreateTemp("", "maiku-cli-*.log")
	if err != nil {
		return "", nil, err
	}
	if m.options.CommandPrefix != "" {
		command = m.options.CommandPrefix + "\n" + command
	}
	cmd := shellcmd.Resolve(m.options.ShellPath).Command(command)
	cmd.Dir = m.cwd
	cmd.Env = os.Environ()
	configureBashCmd(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = log.Close()
		_ = os.Remove(log.Name())
		return "", nil, err
	}
	s := &cliSession{cmd: cmd, stdin: stdin, log: log, path: log.Name(), done: make(chan struct{})}
	reader, writer, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		_ = log.Close()
		_ = os.Remove(log.Name())
		return "", nil, err
	}
	cmd.Stdout, cmd.Stderr = writer, writer
	if err = cmd.Start(); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		_ = stdin.Close()
		_ = log.Close()
		_ = os.Remove(log.Name())
		return "", nil, err
	}
	_ = writer.Close()
	m.next++
	id := strconv.FormatUint(m.next, 10)
	m.sessions[id] = s
	outputDone := make(chan struct{})
	go func() {
		_, copyErr := io.Copy(s, reader)
		_ = reader.Close()
		s.mu.Lock()
		if copyErr != nil && !errors.Is(copyErr, os.ErrClosed) && s.err == nil {
			s.err = copyErr
		}
		s.mu.Unlock()
		close(outputDone)
	}()
	go func() {
		waitErr := cmd.Wait()
		finishOutputPipe(reader, outputDone)
		killProcessGroup(cmd)
		_ = stdin.Close()
		s.mu.Lock()
		s.exitCode = cmd.ProcessState.ExitCode()
		if waitErr != nil && s.exitCode == 0 {
			s.err = waitErr
		}
		_ = log.Close()
		s.mu.Unlock()
		close(s.done)
	}()
	go func() {
		timer := time.NewTimer(*lifetime)
		defer timer.Stop()
		select {
		case <-s.done:
		case <-timer.C:
			s.mu.Lock()
			s.err = errors.New("CLI session timed out")
			s.mu.Unlock()
			killProcessGroup(cmd)
		}
	}()
	return id, s, nil
}

func (m *cliSessions) get(id string) (*cliSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if s == nil {
		return nil, fmt.Errorf("unknown CLI session %q", id)
	}
	return s, nil
}

func (s *cliSession) wait(ctx context.Context, duration time.Duration) error {
	if err := ctx.Err(); err != nil {
		s.stop()
		return err
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		s.stop()
		return ctx.Err()
	case <-s.done:
		return nil
	case <-timer.C:
		return nil
	}
}

func (s *cliSession) stop() {
	select {
	case <-s.done:
	default:
		killProcessGroup(s.cmd)
	}
}

func (s *cliSession) result(id string) agent.AgentToolResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	details := &CLIToolDetails{SessionID: id, Running: true, FullOutputPath: s.path}
	status := "running"
	select {
	case <-s.done:
		details.Running = false
		code := s.exitCode
		details.ExitCode = &code
		status = fmt.Sprintf("exited with code %d", code)
	default:
	}
	text := s.pending
	if s.truncated {
		text = appendStatus(text, "[Output truncated. Full output: "+s.path+"]")
	}
	if s.err != nil {
		status += ": " + s.err.Error()
	}
	text = appendStatus(text, fmt.Sprintf("[CLI session %s: %s]", id, status))
	s.pending, s.truncated = "", false
	return agent.AgentToolResult{Content: []ai.ToolResultContent{{Type: "text", Text: text}}, Details: details}
}

func (m *cliSessions) cliTool() *agent.AgentTool {
	return &agent.AgentTool{Tool: ai.Tool{Name: "cli", Description: "Run and interact with persistent shell commands using stdin/stdout pipes. Start a command, then use its session_id to send input, poll new output, or terminate. Input is exact text; include newline to submit. Returns only new output, keeping the last 400 lines or 16KB; full output is saved to a temp file. Use wait to await completion. Sessions expire after timeout (default 600 seconds).", Parameters: cliSchema}, Label: "cli",
		Execute: func(ctx context.Context, _ string, params map[string]any, _ agent.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
			if err := ctx.Err(); err != nil {
				return agent.AgentToolResult{}, err
			}
			duration, err := boundedWait(params, "yield_ms", 1000, .001)
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			action, _ := argString(params, "action")
			if action == "" {
				action = "start"
			}
			id, _ := argString(params, "session_id")
			var s *cliSession
			if action == "start" {
				id, s, err = m.start(params)
			} else {
				s, err = m.get(id)
			}
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			switch action {
			case "start", "poll":
			case "terminate":
				s.stop()
			case "input":
				input, _ := argString(params, "input")
				closeInput, _ := params["close_stdin"].(bool)
				done := make(chan error, 1)
				go func() {
					s.inputMu.Lock()
					defer s.inputMu.Unlock()
					_, e := io.WriteString(s.stdin, input)
					if e == nil && closeInput {
						e = s.stdin.Close()
					}
					done <- e
				}()
				inputTimer := time.NewTimer(60 * time.Second)
				defer inputTimer.Stop()
				select {
				case err = <-done:
					if err != nil {
						return agent.AgentToolResult{}, err
					}
				case <-ctx.Done():
					s.stop()
					return agent.AgentToolResult{}, ctx.Err()
				case <-inputTimer.C:
					s.stop()
					return agent.AgentToolResult{}, errors.New("CLI stdin write timed out")
				}
			default:
				return agent.AgentToolResult{}, fmt.Errorf("unknown CLI action %q", action)
			}
			if err = s.wait(ctx, duration); err != nil {
				return agent.AgentToolResult{}, err
			}
			return s.result(id), nil
		}}
}

func (m *cliSessions) waitTool(name string) *agent.AgentTool {
	return &agent.AgentTool{Tool: ai.Tool{Name: name, Description: "Wait for a CLI session to complete and return only new output, or pause for seconds without starting a command. Maximum 60 seconds per call. For running sessions, wait again instead of rerunning the command.", Parameters: waitSchema}, Label: name,
		Execute: func(ctx context.Context, _ string, params map[string]any, _ agent.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
			id, _ := argString(params, "session_id")
			if id == "" && argNumberPtr(params, "seconds") == nil {
				return agent.AgentToolResult{}, errors.New("seconds or session_id is required")
			}
			duration, err := boundedWait(params, "seconds", 30, 1)
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			if id != "" {
				s, err := m.get(id)
				if err != nil {
					return agent.AgentToolResult{}, err
				}
				if err := s.wait(ctx, duration); err != nil {
					return agent.AgentToolResult{}, err
				}
				return s.result(id), nil
			}
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return agent.AgentToolResult{}, ctx.Err()
			case <-timer.C:
			}
			return agent.AgentToolResult{Content: []ai.ToolResultContent{{Type: "text", Text: fmt.Sprintf("Waited %g seconds.", duration.Seconds())}}}, nil
		}}
}
