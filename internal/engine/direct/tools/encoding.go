package tools

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// LineEndingKind describes the line-ending style for a file.
type LineEndingKind string

const (
	// LineEndingLF uses Unix line endings.
	LineEndingLF LineEndingKind = "lf"
	// LineEndingCRLF uses Windows line endings.
	LineEndingCRLF LineEndingKind = "crlf"
)

// CharsetKind describes the on-disk charset for a file.
type CharsetKind string

const (
	// CharsetUTF8 stores text as UTF-8.
	CharsetUTF8 CharsetKind = "utf-8"
	// CharsetLatin1 stores text as ISO-8859-1.
	CharsetLatin1 CharsetKind = "latin-1"
	// CharsetCP1252 stores text as Windows-1252.
	CharsetCP1252 CharsetKind = "cp1252"
)

// FileEncoding stores the remembered byte-level encoding for a path.
type FileEncoding struct {
	BOM        bool
	LineEnding LineEndingKind
	Charset    CharsetKind
}

type fileEncodingStore struct {
	mu    sync.RWMutex
	state map[string]FileEncoding
}

var rememberedFileEncodings = fileEncodingStore{
	state: make(map[string]FileEncoding),
}

func rememberFileEncoding(path string, encoding FileEncoding) {
	key := encodingKey(path)

	rememberedFileEncodings.mu.Lock()
	rememberedFileEncodings.state[key] = encoding
	rememberedFileEncodings.mu.Unlock()
}

func lookupFileEncoding(path string) (FileEncoding, bool) {
	key := encodingKey(path)

	rememberedFileEncodings.mu.RLock()
	encoding, ok := rememberedFileEncodings.state[key]
	rememberedFileEncodings.mu.RUnlock()

	return encoding, ok
}

func encodingKey(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}

	return filepath.Clean(abs)
}

func defaultFileEncoding() FileEncoding {
	return FileEncoding{
		LineEnding: LineEndingLF,
		Charset:    CharsetUTF8,
	}
}

func detectAndDecodeText(raw []byte) (string, FileEncoding, error) {
	encoding := defaultFileEncoding()
	if bytes.HasPrefix(raw, utf8BOM) {
		encoding.BOM = true
		raw = raw[len(utf8BOM):]
	}

	if usesCRLF(raw) {
		encoding.LineEnding = LineEndingCRLF
	}

	text, charset, err := decodeText(raw)
	if err != nil {
		return "", FileEncoding{}, err
	}

	encoding.Charset = charset
	return text, encoding, nil
}

func resolveFileEncoding(path, content string, fileExists bool) (FileEncoding, error) {
	if encoding, ok := lookupFileEncoding(path); ok {
		return encoding, nil
	}

	if fileExists {
		raw, err := os.ReadFile(path)
		if err != nil {
			return FileEncoding{}, fmt.Errorf("read file encoding: %w", err)
		}

		_, encoding, err := detectAndDecodeText(raw)
		if err != nil {
			return FileEncoding{}, fmt.Errorf("decode file encoding: %w", err)
		}

		return encoding, nil
	}

	encoding := defaultFileEncoding()
	if strings.Contains(content, "\r\n") {
		encoding.LineEnding = LineEndingCRLF
	}

	return encoding, nil
}

func encodeTextForFile(path, content string, fileExists bool) ([]byte, FileEncoding, error) {
	encoding, err := resolveFileEncoding(path, content, fileExists)
	if err != nil {
		return nil, FileEncoding{}, err
	}

	normalized := content
	if encoding.LineEnding == LineEndingCRLF {
		normalized = normalizeToCRLF(content)
	}

	var encoded string
	switch encoding.Charset {
	case CharsetUTF8:
		encoded = normalized
	case CharsetLatin1:
		encoded, _, err = transform.String(charmap.ISO8859_1.NewEncoder(), normalized)
		if err != nil {
			return nil, FileEncoding{}, fmt.Errorf("encode latin-1: %w", err)
		}
	case CharsetCP1252:
		encoded, _, err = transform.String(charmap.Windows1252.NewEncoder(), normalized)
		if err != nil {
			return nil, FileEncoding{}, fmt.Errorf("encode cp1252: %w", err)
		}
	default:
		return nil, FileEncoding{}, fmt.Errorf("unsupported charset: %s", encoding.Charset)
	}

	data := []byte(encoded)
	if encoding.BOM {
		data = append(append([]byte{}, utf8BOM...), data...)
	}

	return data, encoding, nil
}

func decodeText(raw []byte) (string, CharsetKind, error) {
	if utf8.Valid(raw) {
		return string(raw), CharsetUTF8, nil
	}

	if hasWindows1252Bytes(raw) {
		decoded, _, err := transform.Bytes(charmap.Windows1252.NewDecoder(), raw)
		if err != nil {
			return "", CharsetCP1252, fmt.Errorf("decode cp1252: %w", err)
		}

		return string(decoded), CharsetCP1252, nil
	}

	if hasLatin1Bytes(raw) {
		decoded, _, err := transform.Bytes(charmap.ISO8859_1.NewDecoder(), raw)
		if err != nil {
			return "", CharsetLatin1, fmt.Errorf("decode latin-1: %w", err)
		}

		return string(decoded), CharsetLatin1, nil
	}

	return string(raw), CharsetUTF8, nil
}

func usesCRLF(raw []byte) bool {
	sample := raw
	if len(sample) > 4096 {
		sample = sample[:4096]
	}

	return bytes.Contains(sample, []byte("\r\n"))
}

func hasWindows1252Bytes(raw []byte) bool {
	for _, b := range raw {
		switch b {
		case 0x80, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8A, 0x8B, 0x8C,
			0x8E, 0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98, 0x99, 0x9A, 0x9B,
			0x9C, 0x9E, 0x9F:
			return true
		}
	}

	return false
}

func hasLatin1Bytes(raw []byte) bool {
	for _, b := range raw {
		if b >= 0xA0 {
			return true
		}
	}

	return false
}

func normalizeToCRLF(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.ReplaceAll(normalized, "\n", "\r\n")
}

func looksLikeLegacyText(raw []byte) bool {
	if utf8.Valid(raw) {
		return true
	}

	if hasBinaryControlBytes(raw) {
		return false
	}

	return hasWindows1252Bytes(raw) || hasLatin1Bytes(raw)
}

func hasBinaryControlBytes(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}

	controlCount := 0
	for _, b := range raw {
		if b == '\n' || b == '\r' || b == '\t' {
			continue
		}
		if b < 0x20 || b == 0x7F {
			controlCount++
		}
	}

	return controlCount*5 > len(raw)
}
