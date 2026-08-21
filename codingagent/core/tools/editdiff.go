package tools

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// LineEnding is the dominant line ending style detected in a file.
type LineEnding string

const (
	LineEndingLF   LineEnding = "\n"
	LineEndingCRLF LineEnding = "\r\n"
)

// DetectLineEnding mirrors TS detectLineEnding: looks at whichever of "\n"
// or "\r\n" appears first in the content.
func DetectLineEnding(content string) LineEnding {
	crlfIdx := strings.Index(content, "\r\n")
	lfIdx := strings.Index(content, "\n")
	if lfIdx == -1 {
		return LineEndingLF
	}
	if crlfIdx == -1 {
		return LineEndingLF
	}
	if crlfIdx < lfIdx {
		return LineEndingCRLF
	}
	return LineEndingLF
}

var crlfRe = regexp.MustCompile(`\r\n`)
var crRe = regexp.MustCompile(`\r`)

// NormalizeToLF converts all CRLF/CR line endings to LF.
func NormalizeToLF(text string) string {
	text = crlfRe.ReplaceAllString(text, "\n")
	text = crRe.ReplaceAllString(text, "\n")
	return text
}

// RestoreLineEndings converts LF back to the given ending.
func RestoreLineEndings(text string, ending LineEnding) string {
	if ending == LineEndingCRLF {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}

var (
	smartSingleQuoteRe = regexp.MustCompile("[\u2018\u2019\u201A\u201B]")
	smartDoubleQuoteRe = regexp.MustCompile("[\u201C\u201D\u201E\u201F]")
	unicodeDashRe      = regexp.MustCompile("[\u2010\u2011\u2012\u2013\u2014\u2015\u2212]")
	specialSpaceRe     = regexp.MustCompile("[\u00A0\u2002-\u200A\u202F\u205F\u3000]")
)

// NormalizeForFuzzyMatch normalizes text for fuzzy matching, applying
// progressive transformations: NFKC normalization, trailing-whitespace
// stripping per line, and normalizing smart quotes / Unicode dashes /
// special spaces to their ASCII equivalents.
func NormalizeForFuzzyMatch(text string) string {
	normalized := norm.NFKC.String(text)
	lines := strings.Split(normalized, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRightFunc(line, unicode.IsSpace)
	}
	normalized = strings.Join(lines, "\n")
	normalized = smartSingleQuoteRe.ReplaceAllString(normalized, "'")
	normalized = smartDoubleQuoteRe.ReplaceAllString(normalized, `"`)
	normalized = unicodeDashRe.ReplaceAllString(normalized, "-")
	normalized = specialSpaceRe.ReplaceAllString(normalized, " ")
	return normalized
}

var lineWithEndingRe = regexp.MustCompile(`[^\n]*\n|[^\n]+`)

func splitLinesWithEndings(content string) []string {
	return lineWithEndingRe.FindAllString(content, -1)
}

type lineSpan struct {
	start int
	end   int
}

type matchedEdit struct {
	editIndex   int
	matchIndex  int
	matchLength int
	newText     string
}

type textReplacement struct {
	matchIndex  int
	matchLength int
	newText     string
}

func getLineSpans(content string) []lineSpan {
	offset := 0
	lines := splitLinesWithEndings(content)
	spans := make([]lineSpan, len(lines))
	for i, line := range lines {
		spans[i] = lineSpan{start: offset, end: offset + len(line)}
		offset = spans[i].end
	}
	return spans
}

func getReplacementLineRange(lines []lineSpan, r textReplacement) (startLine, endLine int, err error) {
	replacementStart := r.matchIndex
	replacementEnd := r.matchIndex + r.matchLength

	startLine = -1
	for i, line := range lines {
		if replacementStart >= line.start && replacementStart < line.end {
			startLine = i
			break
		}
	}
	if startLine == -1 {
		return 0, 0, fmt.Errorf("replacement range is outside the base content")
	}

	endLine = startLine
	for endLine < len(lines) && lines[endLine].end < replacementEnd {
		endLine++
	}
	if endLine >= len(lines) {
		return 0, 0, fmt.Errorf("replacement range is outside the base content")
	}

	return startLine, endLine + 1, nil
}

func applyReplacements(content string, replacements []textReplacement, offset int) string {
	result := content
	for _, r := range slices.Backward(replacements) {

		matchIndex := r.matchIndex - offset
		result = result[:matchIndex] + r.newText + result[matchIndex+r.matchLength:]
	}
	return result
}

// applyReplacementsPreservingUnchangedLines applies replacements matched
// against baseContent to originalContent while preserving unchanged line
// blocks from the original.
func applyReplacementsPreservingUnchangedLines(originalContent, baseContent string, replacements []textReplacement) (string, error) {
	originalLines := splitLinesWithEndings(originalContent)
	baseLines := getLineSpans(baseContent)
	if len(originalLines) != len(baseLines) {
		return "", fmt.Errorf("cannot preserve unchanged lines because the base content has a different line count")
	}

	type group struct {
		startLine, endLine int
		replacements       []textReplacement
	}
	var groups []group
	sorted := make([]textReplacement, len(replacements))
	copy(sorted, replacements)
	for i := range sorted {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].matchIndex < sorted[i].matchIndex {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	for _, r := range sorted {
		startLine, endLine, err := getReplacementLineRange(baseLines, r)
		if err != nil {
			return "", err
		}
		if len(groups) > 0 {
			current := &groups[len(groups)-1]
			if startLine < current.endLine {
				if endLine > current.endLine {
					current.endLine = endLine
				}
				current.replacements = append(current.replacements, r)
				continue
			}
		}
		groups = append(groups, group{startLine: startLine, endLine: endLine, replacements: []textReplacement{r}})
	}

	originalLineIndex := 0
	var result strings.Builder
	for _, g := range groups {
		for _, l := range originalLines[originalLineIndex:g.startLine] {
			result.WriteString(l)
		}

		groupStartOffset := baseLines[g.startLine].start
		groupEndOffset := baseLines[g.endLine-1].end
		result.WriteString(applyReplacements(baseContent[groupStartOffset:groupEndOffset], g.replacements, groupStartOffset))
		originalLineIndex = g.endLine
	}
	for _, l := range originalLines[originalLineIndex:] {
		result.WriteString(l)
	}

	return result.String(), nil
}

type fuzzyMatchResult struct {
	found                 bool
	index                 int
	matchLength           int
	usedFuzzyMatch        bool
	contentForReplacement string
}

// fuzzyFindText finds oldText in content, trying exact match first, then
// fuzzy match. When fuzzy matching is used, the returned
// contentForReplacement is the fuzzy-normalized version of the content.
func fuzzyFindText(content, oldText string) fuzzyMatchResult {
	if exactIndex := strings.Index(content, oldText); exactIndex != -1 {
		return fuzzyMatchResult{
			found:                 true,
			index:                 exactIndex,
			matchLength:           len(oldText),
			usedFuzzyMatch:        false,
			contentForReplacement: content,
		}
	}

	fuzzyContent := NormalizeForFuzzyMatch(content)
	fuzzyOldText := NormalizeForFuzzyMatch(oldText)
	fuzzyIndex := strings.Index(fuzzyContent, fuzzyOldText)
	if fuzzyIndex == -1 {
		return fuzzyMatchResult{found: false, index: -1, contentForReplacement: content}
	}

	return fuzzyMatchResult{
		found:                 true,
		index:                 fuzzyIndex,
		matchLength:           len(fuzzyOldText),
		usedFuzzyMatch:        true,
		contentForReplacement: fuzzyContent,
	}
}

// StripBom strips a UTF-8 BOM if present, returning both the BOM (if any)
// and the text without it.
func StripBom(content string) (bom string, text string) {
	if strings.HasPrefix(content, "\uFEFF") {
		return "\uFEFF", content[len("\uFEFF"):]
	}
	return "", content
}

func countOccurrences(content, oldText string) int {
	fuzzyContent := NormalizeForFuzzyMatch(content)
	fuzzyOldText := NormalizeForFuzzyMatch(oldText)
	if fuzzyOldText == "" {
		return 0
	}
	return strings.Count(fuzzyContent, fuzzyOldText)
}

func getNotFoundError(path string, editIndex, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf("could not find the exact text in %s; the old text must match exactly including all whitespace and newlines", path)
	}
	return fmt.Errorf("could not find edits[%d] in %s; oldText must match exactly including all whitespace and newlines", editIndex, path)
}

func getDuplicateError(path string, editIndex, totalEdits, occurrences int) error {
	if totalEdits == 1 {
		return fmt.Errorf("found %d occurrences of the text in %s; the text must be unique; provide more context to make it unique", occurrences, path)
	}
	return fmt.Errorf("found %d occurrences of edits[%d] in %s; each oldText must be unique; provide more context to make it unique", occurrences, editIndex, path)
}

func getEmptyOldTextError(path string, editIndex, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf("oldText must not be empty in %s", path)
	}
	return fmt.Errorf("edits[%d].oldText must not be empty in %s", editIndex, path)
}

func getNoChangeError(path string, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf("no changes made to %s; the replacement produced identical content, which might indicate an issue with special characters or the text not existing as expected", path)
	}
	return fmt.Errorf("no changes made to %s; the replacements produced identical content", path)
}

// Edit is a single targeted oldText -> newText replacement.
type Edit struct {
	OldText string
	NewText string
}

// AppliedEditsResult holds the LF-normalized base and new content after
// applying edits.
type AppliedEditsResult struct {
	BaseContent string
	NewContent  string
}

// ApplyEditsToNormalizedContent applies one or more exact-text replacements
// to LF-normalized content.
//
// All edits are matched against the same original content. Replacements are
// then applied in reverse order so offsets remain stable. If any edit needs
// fuzzy matching, the operation runs in fuzzy-normalized content space and
// then overlays those line-level changes onto the original content so
// unchanged line blocks keep their original bytes.
func ApplyEditsToNormalizedContent(normalizedContent string, edits []Edit, path string) (AppliedEditsResult, error) {
	normalizedEdits := make([]Edit, len(edits))
	for i, e := range edits {
		normalizedEdits[i] = Edit{OldText: NormalizeToLF(e.OldText), NewText: NormalizeToLF(e.NewText)}
	}

	for i, e := range normalizedEdits {
		if len(e.OldText) == 0 {
			return AppliedEditsResult{}, getEmptyOldTextError(path, i, len(normalizedEdits))
		}
	}

	usedFuzzyMatch := false
	for _, e := range normalizedEdits {
		if fuzzyFindText(normalizedContent, e.OldText).usedFuzzyMatch {
			usedFuzzyMatch = true
			break
		}
	}

	replacementBaseContent := normalizedContent
	if usedFuzzyMatch {
		replacementBaseContent = NormalizeForFuzzyMatch(normalizedContent)
	}

	matchedEdits := make([]matchedEdit, 0, len(normalizedEdits))
	for i, e := range normalizedEdits {
		matchResult := fuzzyFindText(replacementBaseContent, e.OldText)
		if !matchResult.found {
			return AppliedEditsResult{}, getNotFoundError(path, i, len(normalizedEdits))
		}

		occurrences := countOccurrences(replacementBaseContent, e.OldText)
		if occurrences > 1 {
			return AppliedEditsResult{}, getDuplicateError(path, i, len(normalizedEdits), occurrences)
		}

		matchedEdits = append(matchedEdits, matchedEdit{
			editIndex:   i,
			matchIndex:  matchResult.index,
			matchLength: matchResult.matchLength,
			newText:     e.NewText,
		})
	}

	for i := 0; i < len(matchedEdits); i++ {
		for j := i + 1; j < len(matchedEdits); j++ {
			if matchedEdits[j].matchIndex < matchedEdits[i].matchIndex {
				matchedEdits[i], matchedEdits[j] = matchedEdits[j], matchedEdits[i]
			}
		}
	}
	for i := 1; i < len(matchedEdits); i++ {
		previous := matchedEdits[i-1]
		current := matchedEdits[i]
		if previous.matchIndex+previous.matchLength > current.matchIndex {
			return AppliedEditsResult{}, fmt.Errorf(
				"edits[%d] and edits[%d] overlap in %s; merge them into one edit or target disjoint regions",
				previous.editIndex, current.editIndex, path,
			)
		}
	}

	replacements := make([]textReplacement, len(matchedEdits))
	for i, m := range matchedEdits {
		replacements[i] = textReplacement{matchIndex: m.matchIndex, matchLength: m.matchLength, newText: m.newText}
	}

	baseContent := normalizedContent
	var newContent string
	if usedFuzzyMatch {
		var err error
		newContent, err = applyReplacementsPreservingUnchangedLines(normalizedContent, replacementBaseContent, replacements)
		if err != nil {
			return AppliedEditsResult{}, err
		}
	} else {
		newContent = applyReplacements(replacementBaseContent, replacements, 0)
	}

	if baseContent == newContent {
		return AppliedEditsResult{}, getNoChangeError(path, len(normalizedEdits))
	}

	return AppliedEditsResult{BaseContent: baseContent, NewContent: newContent}, nil
}

// --- Line diff (Myers algorithm) and unified-ish patch/diff rendering ---
//
// Deviation from TS: the original implementation delegates to the `diff` npm
// package (Diff.diffLines / Diff.createTwoFilesPatch). This port implements
// an equivalent line-based Myers diff directly, so the generated unified
// patch is a standard-format patch but is not guaranteed to be byte-for-byte
// identical to jsdiff's output (e.g. no "Index:"/"===" header lines, no
// "\ No newline at end of file" markers).

type opKind int

const (
	opEqual opKind = iota
	opDelete
	opInsert
)

type diffOp struct {
	Text string
	Kind opKind
}

func myersLineDiff(a, b []string) []diffOp {
	n, m := len(a), len(b)
	max := n + m
	if max == 0 {
		return nil
	}
	offset := max
	size := 2*max + 1
	v := make([]int, size)
	trace := make([][]int, 0, max+1)

	reachedD := -1
outer:
	for d := 0; d <= max; d++ {
		snapshot := make([]int, size)
		copy(snapshot, v)
		trace = append(trace, snapshot)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[offset+k] = x
			if x >= n && y >= m {
				reachedD = d
				break outer
			}
		}
	}

	var ops []diffOp
	x, y := n, m
	for d := reachedD; d > 0; d-- {
		v := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := v[offset+prevK]
		prevY := prevX - prevK

		for x > prevX && y > prevY {
			ops = append(ops, diffOp{Text: a[x-1], Kind: opEqual})
			x--
			y--
		}

		if reachedD > 0 && d > 0 {
			if x == prevX {
				ops = append(ops, diffOp{Text: b[prevY], Kind: opInsert})
			} else {
				ops = append(ops, diffOp{Text: a[prevX], Kind: opDelete})
			}
		}
		x, y = prevX, prevY
	}
	for x > 0 && y > 0 {
		ops = append(ops, diffOp{Text: a[x-1], Kind: opEqual})
		x--
		y--
	}

	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}
	return ops
}

type diffGroup struct {
	Kind  opKind
	Lines []string
}

// splitContentLines tokenizes content into lines without trailing newlines,
// matching jsdiff's diffLines line granularity: a trailing newline at the
// end of content does not produce a phantom empty final line (unlike plain
// strings.Split(content, "\n")).
func splitContentLines(content string) []string {
	tokens := splitLinesWithEndings(content)
	lines := make([]string, len(tokens))
	for i, t := range tokens {
		lines[i] = strings.TrimSuffix(t, "\n")
	}
	return lines
}

func groupDiffOps(ops []diffOp) []diffGroup {
	var groups []diffGroup
	for _, op := range ops {
		if len(groups) > 0 && groups[len(groups)-1].Kind == op.Kind {
			last := &groups[len(groups)-1]
			last.Lines = append(last.Lines, op.Text)
			continue
		}
		groups = append(groups, diffGroup{Kind: op.Kind, Lines: []string{op.Text}})
	}
	return groups
}

// DiffLinesResult mirrors generateDiffString's return shape.
type DiffLinesResult struct {
	Diff             string
	FirstChangedLine int // 0 means "undefined" (no changes)
}

// GenerateDiffString generates a display-oriented diff string with line
// numbers and context. Returns both the diff string and the first changed
// line number (in the new file); FirstChangedLine is 0 when there were no
// changes.
func GenerateDiffString(oldContent, newContent string, contextLines int) DiffLinesResult {
	if contextLines <= 0 {
		contextLines = 4
	}
	oldLines := splitContentLines(oldContent)
	newLines := splitContentLines(newContent)
	ops := myersLineDiff(oldLines, newLines)
	groups := groupDiffOps(ops)
	maxLineNum := max(len(newLines), len(oldLines))
	lineNumWidth := len(fmt.Sprintf("%d", maxLineNum))

	var output []string
	oldLineNum, newLineNum := 1, 1
	lastWasChange := false
	firstChangedLine := 0

	padNum := func(n int) string {
		s := fmt.Sprintf("%d", n)
		for len(s) < lineNumWidth {
			s = " " + s
		}
		return s
	}
	padBlank := func() string {
		return strings.Repeat(" ", lineNumWidth)
	}

	for i, g := range groups {
		raw := g.Lines
		if g.Kind == opInsert || g.Kind == opDelete {
			if firstChangedLine == 0 {
				firstChangedLine = newLineNum
			}
			for _, line := range raw {
				if g.Kind == opInsert {
					output = append(output, fmt.Sprintf("+%s %s", padNum(newLineNum), line))
					newLineNum++
				} else {
					output = append(output, fmt.Sprintf("-%s %s", padNum(oldLineNum), line))
					oldLineNum++
				}
			}
			lastWasChange = true
		} else {
			nextIsChange := i < len(groups)-1 && groups[i+1].Kind != opEqual
			hasLeadingChange := lastWasChange
			hasTrailingChange := nextIsChange

			switch {
			case hasLeadingChange && hasTrailingChange:
				if len(raw) <= contextLines*2 {
					for _, line := range raw {
						output = append(output, fmt.Sprintf(" %s %s", padNum(oldLineNum), line))
						oldLineNum++
						newLineNum++
					}
				} else {
					leading := raw[:contextLines]
					trailing := raw[len(raw)-contextLines:]
					skipped := len(raw) - len(leading) - len(trailing)

					for _, line := range leading {
						output = append(output, fmt.Sprintf(" %s %s", padNum(oldLineNum), line))
						oldLineNum++
						newLineNum++
					}
					output = append(output, fmt.Sprintf(" %s ...", padBlank()))
					oldLineNum += skipped
					newLineNum += skipped
					for _, line := range trailing {
						output = append(output, fmt.Sprintf(" %s %s", padNum(oldLineNum), line))
						oldLineNum++
						newLineNum++
					}
				}
			case hasLeadingChange:
				shown := raw
				if len(shown) > contextLines {
					shown = raw[:contextLines]
				}
				skipped := len(raw) - len(shown)
				for _, line := range shown {
					output = append(output, fmt.Sprintf(" %s %s", padNum(oldLineNum), line))
					oldLineNum++
					newLineNum++
				}
				if skipped > 0 {
					output = append(output, fmt.Sprintf(" %s ...", padBlank()))
					oldLineNum += skipped
					newLineNum += skipped
				}
			case hasTrailingChange:
				skipped := max(len(raw)-contextLines, 0)
				if skipped > 0 {
					output = append(output, fmt.Sprintf(" %s ...", padBlank()))
					oldLineNum += skipped
					newLineNum += skipped
				}
				for _, line := range raw[skipped:] {
					output = append(output, fmt.Sprintf(" %s %s", padNum(oldLineNum), line))
					oldLineNum++
					newLineNum++
				}
			default:
				oldLineNum += len(raw)
				newLineNum += len(raw)
			}
			lastWasChange = false
		}
	}

	return DiffLinesResult{Diff: strings.Join(output, "\n"), FirstChangedLine: firstChangedLine}
}

// GenerateUnifiedPatch generates a standard unified diff patch (see
// deviation note above the line-diff section).
func GenerateUnifiedPatch(path, oldContent, newContent string, contextLines int) string {
	if contextLines <= 0 {
		contextLines = 4
	}
	oldLines := splitContentLines(oldContent)
	newLines := splitContentLines(newContent)
	ops := myersLineDiff(oldLines, newLines)

	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", path, path)
	if len(ops) == 0 {
		return strings.TrimSuffix(out.String(), "\n")
	}

	// oldPos[i]/newPos[i] are the 1-based old/new line numbers "about to be
	// consumed" immediately before ops[i] is processed.
	oldPos := make([]int, len(ops)+1)
	newPos := make([]int, len(ops)+1)
	oldPos[0], newPos[0] = 1, 1
	for i, op := range ops {
		oldPos[i+1] = oldPos[i]
		newPos[i+1] = newPos[i]
		switch op.Kind {
		case opEqual:
			oldPos[i+1]++
			newPos[i+1]++
		case opDelete:
			oldPos[i+1]++
		case opInsert:
			newPos[i+1]++
		}
	}

	var changeIdx []int
	for i, op := range ops {
		if op.Kind != opEqual {
			changeIdx = append(changeIdx, i)
		}
	}
	if len(changeIdx) == 0 {
		return strings.TrimSuffix(out.String(), "\n")
	}

	// Cluster changes into hunks: consecutive changes separated by no more
	// than 2*contextLines of unchanged lines merge into one hunk.
	type window struct{ start, end int }
	var windows []window
	clusterStart, clusterEnd := changeIdx[0], changeIdx[0]
	for k := 1; k < len(changeIdx); k++ {
		idx := changeIdx[k]
		gap := idx - clusterEnd - 1
		if gap <= contextLines*2 {
			clusterEnd = idx
		} else {
			windows = append(windows, window{clusterStart, clusterEnd})
			clusterStart, clusterEnd = idx, idx
		}
	}
	windows = append(windows, window{clusterStart, clusterEnd})

	for _, w := range windows {
		start := max(w.start-contextLines, 0)
		end := min(w.end+contextLines, len(ops)-1)

		oldStart := oldPos[start]
		newStart := newPos[start]
		oldCount := oldPos[end+1] - oldPos[start]
		newCount := newPos[end+1] - newPos[start]
		if oldCount == 0 {
			oldStart = 0
		}
		if newCount == 0 {
			newStart = 0
		}

		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
		for j := start; j <= end; j++ {
			switch ops[j].Kind {
			case opEqual:
				out.WriteString(" ")
			case opDelete:
				out.WriteString("-")
			case opInsert:
				out.WriteString("+")
			}
			out.WriteString(ops[j].Text)
			out.WriteString("\n")
		}
	}

	return strings.TrimSuffix(out.String(), "\n")
}
