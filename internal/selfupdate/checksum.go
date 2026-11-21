package selfupdate

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// ParseChecksums parses a GoReleaser checksums.txt file into a map of
// filename -> lowercase 64-char hex SHA-256 digest. Duplicate entries or
// malformed lines cause a fail-closed error.
func ParseChecksums(data []byte) (map[string]string, error) {
	sums := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("checksums.txt line %d is malformed", lineNo)
		}
		sum := strings.ToLower(fields[0])
		if !isHexSHA256(sum) {
			return nil, fmt.Errorf("checksums.txt line %d has an invalid SHA-256 digest", lineNo)
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == "" {
			return nil, fmt.Errorf("checksums.txt line %d has an empty filename", lineNo)
		}
		if _, exists := sums[name]; exists {
			return nil, fmt.Errorf("checksums.txt has a duplicate entry for %q", name)
		}
		sums[name] = sum
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading checksums.txt: %w", err)
	}
	if len(sums) == 0 {
		return nil, fmt.Errorf("checksums.txt is empty")
	}
	return sums, nil
}

func isHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// VerifyFileSHA256 computes the SHA-256 digest of filePath and compares it
// against expectedHex in constant time. It returns the actual lowercase hex
// digest and a non-nil error on any read failure or checksum mismatch.
func VerifyFileSHA256(filePath, expectedHex string) (string, error) {
	expected := strings.ToLower(strings.TrimSpace(expectedHex))
	if !isHexSHA256(expected) {
		return "", fmt.Errorf("invalid expected SHA-256 checksum %q", expectedHex)
	}
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("opening file for checksum verification: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing downloaded archive: %w", err)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		return actual, fmt.Errorf("SHA-256 checksum mismatch: expected %s, got %s", expected, actual)
	}
	return actual, nil
}
