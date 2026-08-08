// Package tools is a Go port of the coding-agent builtin tool EXECUTE logic
// (TUI rendering is intentionally omitted).
package tools

import (
	"fmt"
	"strings"
)

// Truncation limits. Truncation is based on two independent limits -
// whichever is hit first wins: a line limit and a byte limit. Truncation
// never returns partial lines (except the bash tail-truncation edge case).
const (
	DefaultMaxLines   = 2000
	DefaultMaxBytes   = 50 * 1024 // 50KB
	GrepMaxLineLength = 500       // Max chars per grep match line
)

// TruncatedBy discriminates which limit caused truncation.
type TruncatedBy string

const (
	TruncatedByNone  TruncatedBy = ""
	TruncatedByLines TruncatedBy = "lines"
	TruncatedByBytes TruncatedBy = "bytes"
)

// TruncationResult mirrors TS TruncationResult.
type TruncationResult struct {
	Content               string
	Truncated             bool
	TruncatedBy           TruncatedBy
	TotalLines            int
	TotalBytes            int
	OutputLines           int
	OutputBytes           int
	LastLinePartial       bool
	FirstLineExceedsLimit bool
	MaxLines              int
	MaxBytes              int
}

// TruncationOptions mirrors TS TruncationOptions.
type TruncationOptions struct {
	MaxLines int // 0 means "use default"
	MaxBytes int // 0 means "use default"
}

func resolveTruncationOptions(opts *TruncationOptions) (maxLines, maxBytes int) {
	maxLines = DefaultMaxLines
	maxBytes = DefaultMaxBytes
	if opts != nil {
		if opts.MaxLines != 0 {
			maxLines = opts.MaxLines
		}
		if opts.MaxBytes != 0 {
			maxBytes = opts.MaxBytes
		}
	}
	return
}

func splitLinesForCounting(content string) []string {
	if len(content) == 0 {
		return []string{}
	}
	lines := strings.Split(content, "\n")
	if strings.HasSuffix(content, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// FormatSize formats bytes as a human-readable size.
func FormatSize(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	} else if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
}

// TruncateHead truncates content from the head (keep first N lines/bytes).
// Suitable for file reads where you want to see the beginning.
//
// Never returns partial lines. If the first line exceeds the byte limit,
// returns empty content with FirstLineExceedsLimit=true.
func TruncateHead(content string, opts *TruncationOptions) TruncationResult {
	maxLines, maxBytes := resolveTruncationOptions(opts)

	totalBytes := len(content)
	lines := splitLinesForCounting(content)
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return TruncationResult{
			Content:     content,
			Truncated:   false,
			TruncatedBy: TruncatedByNone,
			TotalLines:  totalLines,
			TotalBytes:  totalBytes,
			OutputLines: totalLines,
			OutputBytes: totalBytes,
			MaxLines:    maxLines,
			MaxBytes:    maxBytes,
		}
	}

	firstLineBytes := len(lines[0])
	if firstLineBytes > maxBytes {
		return TruncationResult{
			Content:               "",
			Truncated:             true,
			TruncatedBy:           TruncatedByBytes,
			TotalLines:            totalLines,
			TotalBytes:            totalBytes,
			OutputLines:           0,
			OutputBytes:           0,
			FirstLineExceedsLimit: true,
			MaxLines:              maxLines,
			MaxBytes:              maxBytes,
		}
	}

	var outputLinesArr []string
	outputBytesCount := 0
	truncatedBy := TruncatedByLines

	for i := 0; i < len(lines) && i < maxLines; i++ {
		line := lines[i]
		lineBytes := len(line)
		if i > 0 {
			lineBytes++ // newline
		}
		if outputBytesCount+lineBytes > maxBytes {
			truncatedBy = TruncatedByBytes
			break
		}
		outputLinesArr = append(outputLinesArr, line)
		outputBytesCount += lineBytes
	}

	if len(outputLinesArr) >= maxLines && outputBytesCount <= maxBytes {
		truncatedBy = TruncatedByLines
	}

	outputContent := strings.Join(outputLinesArr, "\n")
	return TruncationResult{
		Content:     outputContent,
		Truncated:   true,
		TruncatedBy: truncatedBy,
		TotalLines:  totalLines,
		TotalBytes:  totalBytes,
		OutputLines: len(outputLinesArr),
		OutputBytes: len(outputContent),
		MaxLines:    maxLines,
		MaxBytes:    maxBytes,
	}
}

// TruncateTail truncates content from the tail (keep last N lines/bytes).
// Suitable for bash output where you want to see the end (errors, final
// results).
//
// May return a partial first line if the last line of the original content
// exceeds the byte limit.
func TruncateTail(content string, opts *TruncationOptions) TruncationResult {
	maxLines, maxBytes := resolveTruncationOptions(opts)

	totalBytes := len(content)
	lines := splitLinesForCounting(content)
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return TruncationResult{
			Content:     content,
			Truncated:   false,
			TruncatedBy: TruncatedByNone,
			TotalLines:  totalLines,
			TotalBytes:  totalBytes,
			OutputLines: totalLines,
			OutputBytes: totalBytes,
			MaxLines:    maxLines,
			MaxBytes:    maxBytes,
		}
	}

	var outputLinesArr []string
	outputBytesCount := 0
	truncatedBy := TruncatedByLines
	lastLinePartial := false

	for i := len(lines) - 1; i >= 0 && len(outputLinesArr) < maxLines; i-- {
		line := lines[i]
		lineBytes := len(line)
		if len(outputLinesArr) > 0 {
			lineBytes++ // newline
		}

		if outputBytesCount+lineBytes > maxBytes {
			truncatedBy = TruncatedByBytes
			if len(outputLinesArr) == 0 {
				truncatedLine := truncateStringToBytesFromEnd(line, maxBytes)
				outputLinesArr = append([]string{truncatedLine}, outputLinesArr...)
				outputBytesCount = len(truncatedLine)
				lastLinePartial = true
			}
			break
		}

		outputLinesArr = append([]string{line}, outputLinesArr...)
		outputBytesCount += lineBytes
	}

	if len(outputLinesArr) >= maxLines && outputBytesCount <= maxBytes {
		truncatedBy = TruncatedByLines
	}

	outputContent := strings.Join(outputLinesArr, "\n")
	return TruncationResult{
		Content:         outputContent,
		Truncated:       true,
		TruncatedBy:     truncatedBy,
		TotalLines:      totalLines,
		TotalBytes:      totalBytes,
		OutputLines:     len(outputLinesArr),
		OutputBytes:     len(outputContent),
		LastLinePartial: lastLinePartial,
		MaxLines:        maxLines,
		MaxBytes:        maxBytes,
	}
}

// truncateStringToBytesFromEnd truncates a string to fit within a byte limit
// (from the end), respecting UTF-8 character boundaries.
func truncateStringToBytesFromEnd(s string, maxBytes int) string {
	buf := []byte(s)
	if len(buf) <= maxBytes {
		return s
	}
	start := len(buf) - maxBytes
	for start < len(buf) && (buf[start]&0xc0) == 0x80 {
		start++
	}
	return string(buf[start:])
}

// TruncateLine truncates a single line to max characters, adding a
// "[truncated]" suffix. Used for grep match lines.
func TruncateLine(line string, maxChars int) (string, bool) {
	if maxChars <= 0 {
		maxChars = GrepMaxLineLength
	}
	runes := []rune(line)
	if len(runes) <= maxChars {
		return line, false
	}
	return string(runes[:maxChars]) + "... [truncated]", true
}
