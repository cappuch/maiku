package tui

import (
	"fmt"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const composerLines = 3

// Mouse-wheel reports leak into the composer over SSH when a sequence is
// split or not recognized: SGR `<64;col;rowM`, with or without the CSI prefix.
var terminalScrollSequence = regexp.MustCompile(`\x1b\[<\d+;\d+;\d+[Mm]|\x1b\[M[\x20-\x7e]{3}|\[<\d+;\d+;\d+[Mm]|<\d+;\d+;\d+[Mm]`)

func stripTerminalScroll(value string) string {
	return terminalScrollSequence.ReplaceAllString(value, "")
}

func keyIsTerminalScroll(msg tea.KeyMsg) bool {
	return terminalScrollSequence.MatchString(msg.String()) || terminalScrollSequence.MatchString(string(msg.Runes))
}

func (m *model) sanitizeInput() {
	cleaned := stripTerminalScroll(m.input.Value())
	if cleaned == m.input.Value() {
		return
	}
	m.input.SetValue(cleaned)
}

func (m *model) queueView() string {
	if len(m.queue) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s", muted.Render(fmt.Sprintf("%d queued", len(m.queue))))
	shown := min(len(m.queue), 4)
	for i := 0; i < shown; i++ {
		line := strings.Join(strings.Fields(m.queue[i]), " ")
		b.WriteByte('\n')
		b.WriteString(muted.Render(ansi.Truncate(fmt.Sprintf(" %d  %s", i+1, line), max(1, m.width-6), "…")))
	}
	return b.String()
}

// composerView draws the draft from the textarea value. It does not use the
// textarea viewport, which scrolls into its end-of-buffer padding when a
// wheel event arrives as arrow keys over SSH.
func (m *model) composerView() string {
	width := max(1, m.input.Width())
	value := m.input.Value()
	if value == "" {
		return padComposer(muted.Render(ansi.Truncate(m.input.Placeholder, width, "…")), width)
	}
	hard := strings.Split(value, "\n")
	row := m.input.Line()
	if row < 0 || row >= len(hard) {
		row = max(0, len(hard)-1)
	}
	start := 0
	if len(hard) > composerLines {
		start = row - composerLines + 1
		if start < 0 {
			start = 0
		}
		if maxStart := len(hard) - composerLines; start > maxStart {
			start = maxStart
		}
	}
	end := min(len(hard), start+composerLines)
	info := m.input.LineInfo()
	lines := make([]string, 0, composerLines)
	for i := start; i < end; i++ {
		col := 0
		cursor := false
		if i == row {
			col = info.CharOffset
			cursor = true
		}
		lines = append(lines, fitComposerLine(hard[i], col, width, cursor))
	}
	return padComposer(strings.Join(lines, "\n"), width)
}

func fitComposerLine(line string, col, width int, cursor bool) string {
	if width < 1 {
		width = 1
	}
	total := ansi.StringWidth(line)
	if col < 0 {
		col = 0
	}
	if col > total {
		col = total
	}
	start := 0
	if total > width && col >= width {
		start = col - width + 1
	}
	if start < 0 {
		start = 0
	}
	shown := line
	if total > width {
		shown = ansi.Cut(line, start, start+width)
	}
	if !cursor {
		return shown
	}
	local := col - start
	prefix := ansi.Cut(shown, 0, local)
	rest := ansi.Cut(shown, local, ansi.StringWidth(shown))
	ch := " "
	if rest != "" {
		r := []rune(rest)
		ch = string(r[0])
		rest = string(r[1:])
	}
	return prefix + lipgloss.NewStyle().Reverse(true).Render(ch) + rest
}

func padComposer(content string, width int) string {
	lines := strings.Split(content, "\n")
	for len(lines) < composerLines {
		lines = append(lines, "")
	}
	if len(lines) > composerLines {
		lines = lines[:composerLines]
	}
	for i, line := range lines {
		if gap := width - ansi.StringWidth(line); gap > 0 {
			lines[i] = line + strings.Repeat(" ", gap)
		}
	}
	return strings.Join(lines, "\n")
}
