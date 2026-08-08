package core

import (
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/mikus/maiku/ai"
	"github.com/mikus/maiku/codingagent/core/tools"
)

// ProcessedFiles is the result of turning @file paths into prompt text and
// multimodal image attachments.
type ProcessedFiles struct {
	Text   string
	Images []ai.ImageContent
}

// atMentionRe matches @path and @"path with spaces" / @'path with spaces'.
// Paths stop at whitespace (unquoted) or the matching quote (quoted).
var atMentionRe = regexp.MustCompile(`@(?:"([^"]+)"|'([^']+)'|([^\s@]+))`)

// ExtractAtMentions returns unique file paths referenced via @ mentions in
// text, in first-seen order. Leading @ is stripped; quoted paths keep spaces.
func ExtractAtMentions(text string) []string {
	matches := atMentionRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		path := m[1]
		if path == "" {
			path = m[2]
		}
		if path == "" {
			path = m[3]
		}
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}
		// Skip bare punctuation leftovers like "@," if the regex ever over-matches.
		if !hasPathRune(path) {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

func hasPathRune(path string) bool {
	for _, r := range path {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '/' || r == '.' || r == '_' || r == '-' || r == '~' {
			return true
		}
	}
	return false
}

// ProcessFileArguments reads each path relative to cwd and returns prompt text
// plus image attachments. Missing files return an error. Empty files are skipped.
// Image files become ImageContent; everything else is inlined as <file> text.
func ProcessFileArguments(cwd string, fileArgs []string) (ProcessedFiles, error) {
	var textParts []string
	var images []ai.ImageContent

	for _, fileArg := range fileArgs {
		fileArg = strings.TrimSpace(fileArg)
		if fileArg == "" {
			continue
		}
		absolutePath := tools.ResolveReadPath(fileArg, cwd)
		info, err := os.Stat(absolutePath)
		if err != nil {
			return ProcessedFiles{}, fmt.Errorf("failed to read %s: %w", fileArg, err)
		}
		if info.IsDir() {
			return ProcessedFiles{}, fmt.Errorf("%s is a directory", fileArg)
		}
		if info.Size() == 0 {
			continue
		}

		mimeType, _ := tools.DetectSupportedImageMimeTypeFromFile(absolutePath)
		if mimeType != "" {
			data, err := os.ReadFile(absolutePath)
			if err != nil {
				return ProcessedFiles{}, fmt.Errorf("failed to read %s: %w", fileArg, err)
			}
			images = append(images, ai.ImageContent{
				Type:     "image",
				Data:     base64.StdEncoding.EncodeToString(data),
				MimeType: mimeType,
			})
			textParts = append(textParts, fmt.Sprintf("<file path=%q></file>", fileArg))
			continue
		}

		data, err := os.ReadFile(absolutePath)
		if err != nil {
			return ProcessedFiles{}, fmt.Errorf("failed to read %s: %w", fileArg, err)
		}
		textParts = append(textParts, fmt.Sprintf("<file path=%q>\n%s\n</file>", fileArg, string(data)))
	}

	return ProcessedFiles{
		Text:   strings.Join(textParts, "\n\n"),
		Images: images,
	}, nil
}

// ExpandAtMentions finds @path references in message, injects their contents
// ahead of the message, and returns any image attachments. The original message
// text is preserved so mentions remain as human-readable references.
func ExpandAtMentions(cwd, message string) (ProcessedFiles, error) {
	paths := ExtractAtMentions(message)
	if len(paths) == 0 {
		return ProcessedFiles{Text: message}, nil
	}
	processed, err := ProcessFileArguments(cwd, paths)
	if err != nil {
		return ProcessedFiles{}, err
	}
	if processed.Text == "" {
		return ProcessedFiles{Text: message, Images: processed.Images}, nil
	}
	return ProcessedFiles{
		Text:   processed.Text + "\n\n" + message,
		Images: processed.Images,
	}, nil
}
