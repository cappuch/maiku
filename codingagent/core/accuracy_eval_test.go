package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cappuch/maiku/agent"
	"github.com/cappuch/maiku/ai"
)

func TestAgentAccuracy(t *testing.T) {
	if os.Getenv("EVAL_AGENTS") != "1" {
		t.Skip("set EVAL_AGENTS=1 to call the configured model")
	}
	key := cappuKey(t)
	model := ai.Model{
		ID: "deepseek-v4.1-flash", Name: "deepseek-v4.1-flash",
		API: ai.APIOpenAICompletions, Provider: "cappu", BaseURL: "https://api.cappu.ch/v1",
		Reasoning: true, ContextWindow: 128000, MaxTokens: 8192,
		Input: []string{"text"},
	}
	var passed int
	var total ai.Usage
	var requests int
	for _, task := range codingTasks() {
		cwd := materializeTask(t, task)
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
		start := time.Now()
		usage, reqs, stop, err := runMaikuTask(ctx, cwd, key, model, "eval", goFixPrompt, goFixUser, nil)
		cancel()
		ok := testsPass(cwd)
		if ok {
			passed++
		}
		total.Input += usage.Input
		total.Output += usage.Output
		total.CacheRead += usage.CacheRead
		total.CacheWrite += usage.CacheWrite
		total.TotalTokens += usage.TotalTokens
		requests += reqs
		fmt.Printf("maiku  %-8s %s  %s  err=%v  stop=%s  reqs=%d  in=%d out=%d cache_read=%d cache_write=%d hit=%s\n",
			task.name, mark(ok), time.Since(start).Round(time.Millisecond), shortErr(err), stop, reqs,
			usage.Input, usage.Output, usage.CacheRead, usage.CacheWrite, hitRate(usage.Input, usage.CacheRead, usage.CacheWrite))
	}
	fmt.Printf("maiku  accuracy %d/%d  reqs=%d  in=%d out=%d cache_read=%d cache_write=%d hit=%s\n",
		passed, len(codingTasks()), requests, total.Input, total.Output, total.CacheRead, total.CacheWrite,
		hitRate(total.Input, total.CacheRead, total.CacheWrite))
}

func TestAgentHeavy(t *testing.T) {
	if os.Getenv("EVAL_AGENTS") != "1" {
		t.Skip("set EVAL_AGENTS=1 to call the configured model")
	}
	key := cappuKey(t)
	model := ai.Model{
		ID: "deepseek-v4.1-flash", Name: "deepseek-v4.1-flash",
		API: ai.APIOpenAICompletions, Provider: "cappu", BaseURL: "https://api.cappu.ch/v1",
		Reasoning: true, ContextWindow: 128000, MaxTokens: 8192,
		Input: []string{"text"},
	}
	tasks := heavyTasks()
	dirs := make([]string, len(tasks))
	for i, task := range tasks {
		dirs[i] = materializeTask(t, task)
	}
	type row struct {
		name               string
		ok                 bool
		dur                time.Duration
		err                error
		in, out, hit, reqs int
		stop               string
	}
	rows := make([]row, len(tasks))
	var wg sync.WaitGroup
	for i, task := range tasks {
		wg.Add(1)
		go func(i int, task codingTask) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			start := time.Now()
			usage, reqs, stop, err := runMaikuTask(ctx, dirs[i], key, model, "heavy-"+task.name, goFixPrompt, goFixUser, nil)
			cancel()
			rows[i] = row{name: task.name, ok: testsPass(dirs[i]), dur: time.Since(start), err: err, in: usage.Input, out: usage.Output, hit: usage.CacheRead, reqs: reqs, stop: stop}
		}(i, task)
	}
	wg.Wait()
	var passed, in, out, hit, reqs int
	for _, row := range rows {
		if row.ok {
			passed++
		}
		in += row.in
		out += row.out
		hit += row.hit
		reqs += row.reqs
		fmt.Printf("maiku  %-8s %s  %s  err=%v  stop=%s  reqs=%d  in=%d out=%d cache_read=%d hit=%s  cost=%s\n",
			row.name, mark(row.ok), row.dur.Round(time.Millisecond), shortErr(row.err), row.stop, row.reqs,
			row.in, row.out, row.hit, hitRate(row.in, row.hit, 0), offpeak(row.in, row.hit, row.out))
	}
	fmt.Printf("maiku  heavy %d/%d  reqs=%d  in=%d out=%d cache_read=%d hit=%s  cost=%s\n",
		passed, len(tasks), reqs, in, out, hit, hitRate(in, hit, 0), offpeak(in, hit, out))
}

func offpeak(input, cacheRead, output int) string {
	usd := float64(input)*0.15/1e6 + float64(cacheRead)*0.003/1e6 + float64(output)*0.60/1e6
	return fmt.Sprintf("$%.4f", usd)
}

const goFixPrompt = "You are a coding agent. Use tools to inspect and edit the project. Do not modify tests. Run go test and stop when it passes."
const goFixUser = "The tests in this package fail. Fix the implementation so go test passes. Do not modify the tests."

const llamaPrompt = "You are a coding agent. Use tools to inspect and edit this repository. Do not download anything except the single model URL named in the task, and only if that file is missing. Once the verification command named by the user succeeds, stop and report the result. Do not re-read, refactor, or run the command again."

const llamaUser = `This llama.cpp tree has several independent bugs. llama-cli does not successfully generate from the local SmolLM GGUF. A failing run can still exit 0. Success is a generation rate above 0 t/s and no error log. Fix every bug, rebuild, and rerun until that is true.

The model is already at /Users/user/Desktop/bench/SmolLM-135M.Q4_K_S.gguf
If that file is missing you may download only this URL:
https://huggingface.co/mradermacher/SmolLM-135M-GGUF/resolve/main/SmolLM-135M.Q4_K_S.gguf?download=true

Do not download any other file, model, submodule, or package. Do not modify the GGUF.

./build/bin/llama-cli -m /Users/mikus/Desktop/bench/SmolLM-135M.Q4_K_S.gguf -p "Hello" -n 8 --no-warmup -c 128 --single-turn --no-display-prompt`

func TestLlamaBug(t *testing.T) {
	if os.Getenv("EVAL_AGENTS") != "1" {
		t.Skip("set EVAL_AGENTS=1 to call the configured model")
	}
	key := cappuKey(t)
	model := ai.Model{
		ID: "deepseek-v4.1-flash", Name: "deepseek-v4.1-flash",
		API: ai.APIOpenAICompletions, Provider: "cappu", BaseURL: "https://api.cappu.ch/v1",
		Reasoning: true, ContextWindow: 128000, MaxTokens: 8192,
		Input: []string{"text"},
	}
	cwd := "/Users/mikus/Desktop/bench/llama-maiku"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	start := time.Now()
	var heavy time.Duration
	usage, reqs, stop, err := runMaikuTask(ctx, cwd, key, model, "llama", llamaPrompt, llamaUser, &heavy)
	cancel()
	wall := time.Since(start)
	ok := llamaGenerates(cwd)
	fmt.Printf("maiku  llama    %s  agent=%s  heavy=%s  wall=%s  err=%v  stop=%s  reqs=%d  in=%d out=%d cache_read=%d hit=%s  cost=%s\n",
		mark(ok), (wall - heavy).Round(time.Millisecond), heavy.Round(time.Millisecond), wall.Round(time.Millisecond), shortErr(err), stop, reqs,
		usage.Input, usage.Output, usage.CacheRead, hitRate(usage.Input, usage.CacheRead, 0),
		offpeak(usage.Input, usage.CacheRead, usage.Output))
	if !ok {
		t.Errorf("llama-cli still fails")
	}
}

func llamaGenerates(dir string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "./build/bin/llama-cli",
		"-m", "/Users/mikus/Desktop/bench/SmolLM-135M.Q4_K_S.gguf",
		"-p", "Hello", "-n", "8", "--no-warmup", "-c", "128", "--single-turn", "--no-display-prompt")
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	_ = cmd.Run()
	out := buf.String()
	if strings.Contains(out, "positions are decreasing") || strings.Contains(out, "Invalid input batch") || strings.Contains(out, "not a single-token batch") || strings.Contains(out, "out_of_range") {
		return false
	}
	m := regexp.MustCompile(`Generation:\s+([0-9.]+)\s+t/s`).FindStringSubmatch(out)
	if m == nil {
		return false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	return err == nil && v > 0
}

func heavyCommand(cmd string) bool {
	c := strings.ToLower(cmd)
	if strings.HasPrefix(strings.TrimSpace(c), "make") {
		return true
	}
	for _, key := range []string{"cmake", "ninja", "clang", "g++", "c++", "llama-cli"} {
		if strings.Contains(c, key) {
			return true
		}
	}
	return false
}

func runMaikuTask(ctx context.Context, cwd, key string, model ai.Model, sessionID, systemPrompt, userPrompt string, heavy *time.Duration) (ai.Usage, int, string, error) {
	session := NewAgentSession(AgentSessionOptions{
		Model: model, APIKey: key, ThinkingLevel: agent.ThinkingLow, SessionID: sessionID,
		SystemPrompt: systemPrompt,
		Tools:        SelectToolsWithOptions(cwd, []string{"read", "edit", "write", "bash"}, nil, false, ToolOptions{}),
	})
	defer session.Dispose()
	if heavy != nil {
		var mu sync.Mutex
		starts := map[string]time.Time{}
		session.Subscribe(func(ev agent.AgentEvent) {
			if ev.ToolName != "bash" {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			switch ev.Type {
			case agent.EventToolExecutionStart:
				cmd, _ := ev.Args["command"].(string)
				if heavyCommand(cmd) {
					starts[ev.ToolCallID] = time.Now()
				}
			case agent.EventToolExecutionEnd:
				if t0, ok := starts[ev.ToolCallID]; ok {
					*heavy += time.Since(t0)
					delete(starts, ev.ToolCallID)
				}
			}
		})
	}
	err := session.Prompt(ctx, userPrompt)
	usage, reqs, stop := maikuUsage(session)
	return usage, reqs, stop, err
}

func maikuUsage(session *AgentSession) (ai.Usage, int, string) {
	total := ai.EmptyUsage()
	var reqs int
	var stop string
	for _, message := range session.State().Messages {
		assistant, ok := message.AsAssistant()
		if !ok {
			continue
		}
		u := assistant.Usage
		if u.Input == 0 && u.Output == 0 && u.CacheRead == 0 && u.CacheWrite == 0 && u.TotalTokens == 0 {
			continue
		}
		reqs++
		total.Input += u.Input
		total.Output += u.Output
		total.CacheRead += u.CacheRead
		total.CacheWrite += u.CacheWrite
		total.TotalTokens += u.TotalTokens
		stop = string(assistant.StopReason)
	}
	return total, reqs, stop
}

func hitRate(input, cacheRead, cacheWrite int) string {
	total := input + cacheRead + cacheWrite
	if total == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(cacheRead)/float64(total))
}

func cappuKey(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	var auth map[string]struct {
		Key string `json:"key"`
	}
	raw, err := os.ReadFile(filepath.Join(home, ".maiku", "agent", "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &auth); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(auth["cappu"].Key) == "" {
		t.Fatal("no cappu key")
	}
	return auth["cappu"].Key
}

type codingTask struct {
	name  string
	files map[string]string
}

func TestFixturesFail(t *testing.T) {
	if len(codingTasks()) != 10 {
		t.Fatalf("got %d tasks", len(codingTasks()))
	}
	for _, task := range codingTasks() {
		if testsPass(materializeTask(t, task)) {
			t.Errorf("%s already passes", task.name)
		}
	}
}

func TestHeavyFixturesFail(t *testing.T) {
	if len(heavyTasks()) != 6 {
		t.Fatalf("got %d tasks", len(heavyTasks()))
	}
	for _, task := range heavyTasks() {
		if testsPass(materializeTask(t, task)) {
			t.Errorf("%s already passes", task.name)
		}
	}
}

func materializeTask(t *testing.T, task codingTask) string {
	t.Helper()
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "go.mod"), []byte("module example.com/p\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, body := range task.files {
		if err := os.WriteFile(filepath.Join(cwd, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return cwd
}

func testsPass(cwd string) bool {
	cmd := exec.Command("go", "test")
	cmd.Dir = cwd
	return cmd.Run() == nil
}

func mark(ok bool) string {
	if ok {
		return "pass"
	}
	return "fail"
}

func shortErr(err error) string {
	if err == nil {
		return "nil"
	}
	s := err.Error()
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}
