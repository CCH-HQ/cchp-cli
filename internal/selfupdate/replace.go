package selfupdate

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/CCH-HQ/cchp-cli/internal/contract"
)

// ReplaceBinary atomically replaces targetPath with the contents of srcPath.
// The replacement is staged in a temporary file inside targetPath's directory,
// synced, chmodded to 0755, and renamed into place. On Windows, the running
// binary is first moved aside to targetPath+".old" with automatic rollback on
// failure.
func ReplaceBinary(targetPath, srcPath string) error {
	return replaceBinary(targetPath, srcPath, runtime.GOOS == "windows")
}

func replaceBinary(targetPath, srcPath string, windowsMode bool) error {
	if targetPath == "" {
		return contract.Fail(contract.KindInternal, contract.ExitInternal, "Cannot replace binary: executable path is empty.")
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return contract.Fail(contract.KindInternal, contract.ExitInternal, "Opening extracted binary: %v", err)
	}
	defer func() { _ = src.Close() }()

	dir := filepath.Dir(targetPath)
	tmp, err := os.CreateTemp(dir, ".cchp-update-*")
	if err != nil {
		return replaceFailure(targetPath, dir, err)
	}
	tmpPath := tmp.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		return replaceFailure(targetPath, dir, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return replaceFailure(targetPath, dir, err)
	}
	if !windowsMode {
		if err := tmp.Chmod(0o755); err != nil {
			_ = tmp.Close()
			return replaceFailure(targetPath, dir, err)
		}
	}
	if err := tmp.Close(); err != nil {
		return replaceFailure(targetPath, dir, err)
	}

	if windowsMode {
		if err := swapWindowsBinary(targetPath, tmpPath); err != nil {
			return replaceFailure(targetPath, dir, err)
		}
		cleanupTmp = false
		return nil
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		return replaceFailure(targetPath, dir, err)
	}
	cleanupTmp = false
	return nil
}

func swapWindowsBinary(targetPath, tmpPath string) error {
	oldPath := targetPath + ".old"
	_ = os.Remove(oldPath)

	movedOld := false
	if _, err := os.Stat(targetPath); err == nil {
		if err := os.Rename(targetPath, oldPath); err != nil {
			return err
		}
		movedOld = true
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		if movedOld {
			// Roll back so the original binary remains usable.
			_ = os.Rename(oldPath, targetPath)
		}
		return err
	}

	// Best effort: Windows keeps the running .old file locked until process exit.
	if movedOld {
		_ = os.Remove(oldPath)
	}
	return nil
}

func replaceFailure(targetPath, dir string, err error) *contract.Failure {
	if errors.Is(err, os.ErrPermission) {
		return contract.Fail(contract.KindPermission, contract.ExitPermission, "Permission denied while updating %s (cannot write to %s).", targetPath, dir).
			WithHint("Check write permissions on %s (for example, re-run with elevated permissions or install into ~/.local/bin).", dir)
	}
	return contract.Fail(contract.KindInternal, contract.ExitInternal, "Failed to replace %s: %v", targetPath, err)
}
