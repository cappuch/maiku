package tools

import (
	"bytes"
	"errors"
	"io"
	"os"
)

const imageSniffBytes = 4100

var pngSignature = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

// DetectSupportedImageMimeType sniffs magic bytes to identify a supported
// image type (jpeg, png, gif, webp, bmp). Returns "" when unrecognized.
//
// Deviation from TS: animated PNG detection and BMP validation are ported;
// image resizing/re-encoding (image-process.ts / image-resize.ts) is not
// ported, so BMP images pass through undetected here as unsupported for
// inline transmission (see read.go).
func DetectSupportedImageMimeType(buf []byte) string {
	if bytesStartWith(buf, []byte{0xff, 0xd8, 0xff}) {
		if len(buf) > 3 && buf[3] == 0xf7 {
			return ""
		}
		return "image/jpeg"
	}
	if bytesStartWith(buf, pngSignature) {
		if isPng(buf) && !isAnimatedPng(buf) {
			return "image/png"
		}
		return ""
	}
	if bytesStartWithAscii(buf, 0, "GIF") {
		return "image/gif"
	}
	if bytesStartWithAscii(buf, 0, "RIFF") && bytesStartWithAscii(buf, 8, "WEBP") {
		return "image/webp"
	}
	if bytesStartWithAscii(buf, 0, "BM") && isBmp(buf) {
		return "image/bmp"
	}
	return ""
}

// DetectSupportedImageMimeTypeFromFile sniffs the first bytes of a file.
func DetectSupportedImageMimeTypeFromFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, imageSniffBytes)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		if errors.Is(err, io.EOF) {
			return "", nil
		}
		return "", err
	}
	return DetectSupportedImageMimeType(buf[:n]), nil
}

func isPng(buf []byte) bool {
	return len(buf) >= 16 && readUint32BE(buf, len(pngSignature)) == 13 && bytesStartWithAscii(buf, 12, "IHDR")
}

func isAnimatedPng(buf []byte) bool {
	offset := len(pngSignature)
	for offset+8 <= len(buf) {
		chunkLength := int(readUint32BE(buf, offset))
		chunkTypeOffset := offset + 4
		if bytesStartWithAscii(buf, chunkTypeOffset, "acTL") {
			return true
		}
		if bytesStartWithAscii(buf, chunkTypeOffset, "IDAT") {
			return false
		}
		nextOffset := offset + 8 + chunkLength + 4
		if nextOffset <= offset || nextOffset > len(buf) {
			return false
		}
		offset = nextOffset
	}
	return false
}

func isBmp(buf []byte) bool {
	if len(buf) < 26 {
		return false
	}
	declaredFileSize := readUint32LE(buf, 2)
	pixelDataOffset := readUint32LE(buf, 10)
	dibHeaderSize := readUint32LE(buf, 14)
	if declaredFileSize != 0 && declaredFileSize < 26 {
		return false
	}
	if pixelDataOffset < 14+dibHeaderSize {
		return false
	}
	if declaredFileSize != 0 && pixelDataOffset >= declaredFileSize {
		return false
	}

	var colorPlanes, bitsPerPixel uint32
	if dibHeaderSize == 12 {
		colorPlanes = uint32(readUint16LE(buf, 22))
		bitsPerPixel = uint32(readUint16LE(buf, 24))
	} else if dibHeaderSize >= 40 && dibHeaderSize <= 124 {
		if len(buf) < 30 {
			return false
		}
		colorPlanes = uint32(readUint16LE(buf, 26))
		bitsPerPixel = uint32(readUint16LE(buf, 28))
	} else {
		return false
	}

	if colorPlanes != 1 {
		return false
	}
	switch bitsPerPixel {
	case 1, 4, 8, 16, 24, 32:
		return true
	default:
		return false
	}
}

func readUint16LE(buf []byte, offset int) uint16 {
	return uint16(safeByte(buf, offset)) | uint16(safeByte(buf, offset+1))<<8
}

func readUint32BE(buf []byte, offset int) uint32 {
	return uint32(safeByte(buf, offset))<<24 | uint32(safeByte(buf, offset+1))<<16 |
		uint32(safeByte(buf, offset+2))<<8 | uint32(safeByte(buf, offset+3))
}

func readUint32LE(buf []byte, offset int) uint32 {
	return uint32(safeByte(buf, offset)) | uint32(safeByte(buf, offset+1))<<8 |
		uint32(safeByte(buf, offset+2))<<16 | uint32(safeByte(buf, offset+3))<<24
}

func safeByte(buf []byte, offset int) byte {
	if offset < 0 || offset >= len(buf) {
		return 0
	}
	return buf[offset]
}

func bytesStartWith(buf, prefix []byte) bool {
	if len(buf) < len(prefix) {
		return false
	}
	return bytes.Equal(buf[:len(prefix)], prefix)
}

func bytesStartWithAscii(buf []byte, offset int, text string) bool {
	if len(buf) < offset+len(text) {
		return false
	}
	for i := 0; i < len(text); i++ {
		if buf[offset+i] != text[i] {
			return false
		}
	}
	return true
}
