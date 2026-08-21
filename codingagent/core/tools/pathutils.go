package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// narrowNoBreakSpace is used to fix up macOS screenshot filenames that use
// U+202F before AM/PM.
const narrowNoBreakSpace = "\u202f"

var unicodeSpacesRe = regexp.MustCompile(
	"[\u00A0\u1680\u2000-\u200A\u2028\u2029\u202F\u205F\u3000]",
)

var amPmRe = regexp.MustCompile(`(?i) (AM|PM)\.`)

func tryMacOSScreenshotPath(filePath string) string {
	return amPmRe.ReplaceAllString(filePath, narrowNoBreakSpace+"$1.")
}

func tryNFDVariant(filePath string) string {
	return norm.NFD.String(filePath)
}

func tryCurlyQuoteVariant(filePath string) string {
	return strings.ReplaceAll(filePath, "'", "\u2019")
}

// PathExists reports whether filePath exists on disk.
func PathExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

// ExpandPath normalizes unicode spaces, strips a leading "@", and expands a
// leading "~" to the user's home directory.
func ExpandPath(filePath string) string {
	return normalizePath(filePath)
}

func normalizePath(input string) string {
	normalized := unicodeSpacesRe.ReplaceAllString(input, " ")
	normalized = strings.TrimPrefix(normalized, "@")

	if normalized == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return normalized
	}
	if strings.HasPrefix(normalized, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, normalized[2:])
		}
	}

	return normalized
}

// ResolveToCwd resolves a path relative to the given cwd. Handles ~
// expansion and absolute paths.
func ResolveToCwd(filePath string, cwd string) string {
	normalized := normalizePath(filePath)
	normalizedCwd := normalizePath(cwd)
	if filepath.IsAbs(normalized) {
		return filepath.Clean(normalized)
	}
	return filepath.Clean(filepath.Join(normalizedCwd, normalized))
}

// ResolveReadPath resolves a read path, trying a handful of macOS-specific
// filename variants (AM/PM narrow space, NFD normalization, curly quotes)
// when the direct resolution does not exist.
func ResolveReadPath(filePath string, cwd string) string {
	resolved := ResolveToCwd(filePath, cwd)
	if PathExists(resolved) {
		return resolved
	}

	amPmVariant := tryMacOSScreenshotPath(resolved)
	if amPmVariant != resolved && PathExists(amPmVariant) {
		return amPmVariant
	}

	nfdVariant := tryNFDVariant(resolved)
	if nfdVariant != resolved && PathExists(nfdVariant) {
		return nfdVariant
	}

	curlyVariant := tryCurlyQuoteVariant(resolved)
	if curlyVariant != resolved && PathExists(curlyVariant) {
		return curlyVariant
	}

	nfdCurlyVariant := tryCurlyQuoteVariant(nfdVariant)
	if nfdCurlyVariant != resolved && PathExists(nfdCurlyVariant) {
		return nfdCurlyVariant
	}

	return resolved
}
