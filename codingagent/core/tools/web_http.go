package tools

import (
	"net/http"
	"strings"
	"unicode/utf8"
)

// BrowserUserAgent is sent by the web tools so sites return their normal
// desktop HTML rather than bot- or library-specific responses.
const BrowserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36"

const browserAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"

func setBrowserHeaders(header http.Header) {
	header.Set("Accept", browserAccept)
	header.Set("Accept-Language", "en-GB,en-US;q=0.9,en;q=0.8")
	header.Set("Cache-Control", "max-age=0")
	header.Set("Upgrade-Insecure-Requests", "1")
	header.Set("User-Agent", BrowserUserAgent)
}

func truncateWebContent(content string) (string, bool) {
	cut := len(content)
	if cut > DefaultMaxBytes {
		cut = DefaultMaxBytes
	}

	lineEnd := 0
	for i := 0; i < len(content) && lineEnd < DefaultMaxLines; i++ {
		if content[i] == '\n' {
			lineEnd++
			if lineEnd == DefaultMaxLines && i+1 < cut {
				cut = i + 1
			}
		}
	}

	truncated := cut < len(content)
	for cut > 0 && cut < len(content) && !utf8.RuneStart(content[cut]) {
		cut--
	}
	return strings.ToValidUTF8(content[:cut], "�"), truncated
}
