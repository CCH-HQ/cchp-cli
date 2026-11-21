package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// maxBinaryBytes bounds the extracted cchp executable to guard against
// decompression bombs.
const maxBinaryBytes = 256 << 20 // 256 MiB

// ExtractBinary extracts binaryName (e.g. "cchp" or "cchp.exe") from a .tar.gz
// or .zip archive into dstPath. Every archive entry is validated against path
// traversal before anything is accepted.
func ExtractBinary(archivePath, binaryName, dstPath string) error {
	switch {
	case strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".tgz"):
		return extractFromTarGz(archivePath, binaryName, dstPath)
	case strings.HasSuffix(archivePath, ".zip"):
		return extractFromZip(archivePath, binaryName, dstPath)
	default:
		return fmt.Errorf("unsupported archive format %q", archivePath)
	}
}

// validateArchiveEntryPath rejects absolute paths, backslashes, drive letters,
// and any ".." path traversal segment.
func validateArchiveEntryPath(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("archive contains an empty entry path")
	}
	if strings.Contains(raw, "\\") {
		return "", fmt.Errorf("archive contains unsafe backslash path %q", raw)
	}
	if strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("archive contains unsafe absolute path %q", raw)
	}
	// Guard against Windows drive-letter paths such as "C:foo" or "C:/foo".
	if len(raw) >= 2 && ((raw[0] >= 'a' && raw[0] <= 'z') || (raw[0] >= 'A' && raw[0] <= 'Z')) && raw[1] == ':' {
		return "", fmt.Errorf("archive contains unsafe drive path %q", raw)
	}
	for seg := range strings.SplitSeq(raw, "/") {
		if seg == ".." {
			return "", fmt.Errorf("archive contains path traversal entry %q", raw)
		}
	}
	cleaned := path.Clean(raw)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("archive contains path traversal entry %q", raw)
	}
	return cleaned, nil
}

func extractFromTarGz(archivePath, binaryName, dstPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("opening tar.gz archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("reading gzip stream: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	found := false
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar archive: %w", err)
		}
		cleaned, err := validateArchiveEntryPath(hdr.Name)
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
			if _, err := validateArchiveEntryPath(hdr.Linkname); err != nil {
				return fmt.Errorf("archive symlink %q has unsafe target: %w", hdr.Name, err)
			}
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if cleaned != binaryName {
			continue
		}
		if found {
			return fmt.Errorf("archive contains multiple %q binaries", binaryName)
		}
		if err := writeBoundedFile(dstPath, tr, maxBinaryBytes); err != nil {
			return err
		}
		found = true
	}
	if !found {
		return fmt.Errorf("archive does not contain %q", binaryName)
	}
	return nil
}

func extractFromZip(archivePath, binaryName, dstPath string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("opening zip archive: %w", err)
	}
	defer func() { _ = zr.Close() }()

	var target *zip.File
	for _, zf := range zr.File {
		cleaned, err := validateArchiveEntryPath(zf.Name)
		if err != nil {
			return err
		}
		if zf.FileInfo().IsDir() || !zf.Mode().IsRegular() {
			continue
		}
		if cleaned != binaryName {
			continue
		}
		if target != nil {
			return fmt.Errorf("archive contains multiple %q binaries", binaryName)
		}
		target = zf
	}
	if target == nil {
		return fmt.Errorf("archive does not contain %q", binaryName)
	}
	rc, err := target.Open()
	if err != nil {
		return fmt.Errorf("opening %q in zip archive: %w", binaryName, err)
	}
	defer func() { _ = rc.Close() }()

	return writeBoundedFile(dstPath, rc, maxBinaryBytes)
}

func writeBoundedFile(dstPath string, r io.Reader, limit int64) error {
	out, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return fmt.Errorf("creating extracted binary file: %w", err)
	}
	n, copyErr := io.Copy(out, io.LimitReader(r, limit+1))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dstPath)
		return fmt.Errorf("extracting binary: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(dstPath)
		return fmt.Errorf("closing extracted binary: %w", closeErr)
	}
	if n == 0 {
		_ = os.Remove(dstPath)
		return errors.New("extracted binary is empty")
	}
	if n > limit {
		_ = os.Remove(dstPath)
		return fmt.Errorf("extracted binary exceeds the %d MiB safety limit", limit>>20)
	}
	return nil
}
