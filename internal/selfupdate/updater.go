package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/redact"
)

// CommandRunner executes external update tools (brew, go) and is injectable
// for unit testing.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr string, err error)
}

// ExecRunner runs commands via os/exec.
type ExecRunner struct{}

// Run executes name with args under ctx and captures stdout and stderr.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// Config configures an Updater. Zero fields use production defaults.
type Config struct {
	CurrentVersion string
	UserAgent      string
	APIBaseURL     string
	Repo           string
	HTTPClient     *http.Client
	RetryMax       *int
	CmdRunner      CommandRunner
	Executable     func() (string, error)
	EvalSymlinks   func(string) (string, error)
	Getenv         func(string) string
	LookPath       func(string) (string, error)
	UserHomeDir    func() (string, error)
	GOOS           string
	GOARCH         string
	Debug          io.Writer
	OnProgress     func(string)
}

// RunOptions controls one invocation of Updater.Run.
type RunOptions struct {
	Check   bool
	Force   bool
	DryRun  bool
	Version string
}

// Result describes the outcome of cchp update.
type Result struct {
	CurrentVersion  string   `json:"current_version"`
	LatestVersion   string   `json:"latest_version,omitempty"`
	TargetVersion   string   `json:"target_version,omitempty"`
	InstallMethod   Method   `json:"install_method"`
	EffectiveMethod Method   `json:"effective_method,omitempty"`
	ExecutablePath  string   `json:"executable_path,omitempty"`
	UpdateAvailable bool     `json:"update_available"`
	Updated         bool     `json:"updated"`
	UpToDate        bool     `json:"up_to_date,omitempty"`
	CheckOnly       bool     `json:"check_only,omitempty"`
	DryRun          bool     `json:"dry_run,omitempty"`
	DevBuild        bool     `json:"dev_build,omitempty"`
	NeedsForce      bool     `json:"needs_force,omitempty"`
	Action          string   `json:"action,omitempty"`
	PlannedCommand  []string `json:"planned_command,omitempty"`
	Command         []string `json:"command,omitempty"`
	ArchiveName     string   `json:"archive_name,omitempty"`
	DownloadURL     string   `json:"download_url,omitempty"`
	ChecksumsURL    string   `json:"checksums_url,omitempty"`
	SHA256          string   `json:"sha256,omitempty"`
	ReleaseURL      string   `json:"release_url,omitempty"`
	DocsURL         string   `json:"docs_url"`
	Message         string   `json:"message,omitempty"`
}

// Updater detects the install method and safely updates cchp.
type Updater struct {
	cfg Config
}

// New constructs an Updater with production defaults for unset fields.
func New(cfg Config) *Updater {
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = DefaultAPIBaseURL
	}
	if cfg.Repo == "" {
		cfg.Repo = DefaultRepo
	}
	if cfg.UserAgent == "" {
		v := strings.TrimSpace(cfg.CurrentVersion)
		if v == "" {
			v = "dev"
		}
		cfg.UserAgent = "cchp/" + v
	}
	if cfg.CmdRunner == nil {
		cfg.CmdRunner = ExecRunner{}
	}
	if cfg.Executable == nil {
		cfg.Executable = os.Executable
	}
	if cfg.EvalSymlinks == nil {
		cfg.EvalSymlinks = filepath.EvalSymlinks
	}
	if cfg.Getenv == nil {
		cfg.Getenv = os.Getenv
	}
	if cfg.LookPath == nil {
		cfg.LookPath = exec.LookPath
	}
	if cfg.UserHomeDir == nil {
		cfg.UserHomeDir = os.UserHomeDir
	}
	if cfg.GOOS == "" {
		cfg.GOOS = runtime.GOOS
	}
	if cfg.GOARCH == "" {
		cfg.GOARCH = runtime.GOARCH
	}
	return &Updater{cfg: cfg}
}

// Run executes the update check, dry-run plan, or actual update.
// Every returned error is a *contract.Failure whose Hint includes concrete
// manual upgrade instructions and the locale-appropriate official docs URL.
func (u *Updater) Run(ctx context.Context, opts RunOptions) (*Result, error) {
	docsURL := DocsURL(u.cfg.Getenv)
	currentDisplay := DisplayVersion(u.cfg.CurrentVersion)

	execPath, execErr := u.cfg.Executable()
	var det Detection
	if execErr == nil {
		det = Detect(execPath, u.cfg.CurrentVersion, DetectEnv{
			Getenv:       u.cfg.Getenv,
			LookPath:     u.cfg.LookPath,
			EvalSymlinks: u.cfg.EvalSymlinks,
			UserHomeDir:  u.cfg.UserHomeDir,
			GOOS:         u.cfg.GOOS,
		})
	} else {
		det = Detection{
			Method:           MethodDirect,
			UnderlyingMethod: MethodDirect,
			IsDev:            !IsReleaseVersion(u.cfg.CurrentVersion),
		}
		if det.IsDev {
			det.Method = MethodDev
		}
	}

	fail := func(f *contract.Failure, targetVer, extra string) error {
		return withGuidance(f, det.Method, targetVer, docsURL, u.cfg.GOOS, extra)
	}

	// Validate --version up front so bad input fails fast with docs guidance.
	var requestedTag string
	if strings.TrimSpace(opts.Version) != "" {
		norm, ok := NormalizeVersion(opts.Version)
		if !ok {
			return nil, fail(
				contract.Fail(contract.KindUsage, contract.ExitUsage, "--version %q is not a valid release version (expected vX.Y.Z, such as v0.2.0).", opts.Version),
				"",
				"",
			)
		}
		requestedTag = norm
	}

	// Dev build guard: refuse to overwrite unless --force (allow --check and --dry-run to inspect safely).
	if det.IsDev && !opts.Check && !opts.DryRun && !opts.Force {
		return nil, fail(
			contract.Fail(contract.KindPolicy, contract.ExitPolicy, "Current cchp binary is a development build (%s); refusing to overwrite without --force.", currentDisplay),
			requestedTag,
			"",
		)
	}

	effectiveMethod := det.Method
	if det.IsDev && (opts.Force || opts.DryRun) {
		effectiveMethod = det.UnderlyingMethod
	}

	// Homebrew formulas always track the latest tap release, not arbitrary tags.
	if requestedTag != "" && effectiveMethod == MethodHomebrew && !opts.Check {
		return nil, fail(
			contract.Fail(contract.KindUsage, contract.ExitUsage, "--version %s is not supported for Homebrew installs; Homebrew always upgrades to the latest tap release.", requestedTag),
			requestedTag,
			"To pin a specific version, install directly with install.sh instead of Homebrew.",
		)
	}

	if execErr != nil && !opts.Check && !opts.DryRun && effectiveMethod == MethodDirect {
		return nil, fail(
			contract.Fail(contract.KindInternal, contract.ExitInternal, "Could not determine the path to the running cchp executable: %v", execErr),
			requestedTag,
			"",
		)
	}

	retryMax := 2
	if u.cfg.RetryMax != nil {
		retryMax = *u.cfg.RetryMax
	}
	client := newReleaseClient(u.cfg.APIBaseURL, u.cfg.Repo, u.cfg.UserAgent, u.cfg.HTTPClient, retryMax, u.cfg.Debug)

	u.progress("Checking latest cchp release…")
	rel, err := client.fetchRelease(ctx, requestedTag)
	if err != nil {
		return nil, fail(asFailure(err), requestedTag, "")
	}

	targetVersion := rel.TagName
	updateAvailable := false
	upToDate := false
	if det.IsDev {
		updateAvailable = true
	} else if cmp, ok := CompareVersions(u.cfg.CurrentVersion, targetVersion); ok {
		if requestedTag != "" {
			updateAvailable = cmp != 0
			upToDate = cmp == 0
		} else {
			updateAvailable = cmp < 0
			upToDate = cmp >= 0
		}
	}

	res := &Result{
		CurrentVersion:  currentDisplay,
		LatestVersion:   targetVersion,
		TargetVersion:   targetVersion,
		InstallMethod:   det.Method,
		ExecutablePath:  det.ExecutablePath,
		UpdateAvailable: updateAvailable,
		UpToDate:        upToDate,
		CheckOnly:       opts.Check,
		DryRun:          opts.DryRun,
		DevBuild:        det.IsDev,
		ReleaseURL:      rel.HTMLURL,
		DocsURL:         docsURL,
	}
	if det.IsDev && effectiveMethod != det.Method {
		res.EffectiveMethod = effectiveMethod
	}

	// --check: report status and stop without modifying anything.
	if opts.Check {
		switch {
		case det.IsDev:
			res.NeedsForce = true
			res.Message = fmt.Sprintf("Development build (%s); latest release is %s. Re-run with --force to replace it.", currentDisplay, targetVersion)
		case updateAvailable:
			res.Message = fmt.Sprintf("Update available: %s -> %s.", currentDisplay, targetVersion)
		default:
			res.Message = fmt.Sprintf("cchp %s is up to date.", currentDisplay)
		}
		return res, nil
	}

	// Already up to date (unless --force).
	if upToDate && !opts.Force {
		res.Action = "none"
		res.Message = fmt.Sprintf("cchp %s is already up to date.", currentDisplay)
		return res, nil
	}

	// --dry-run: show the exact planned action without executing commands or modifying files.
	if opts.DryRun {
		if det.IsDev && !opts.Force {
			res.NeedsForce = true
		}
		return u.planDryRun(res, effectiveMethod, rel, docsURL)
	}

	switch effectiveMethod {
	case MethodHomebrew:
		return u.updateHomebrew(ctx, res, opts.Force, docsURL)
	case MethodGoInstall:
		return u.updateGoInstall(ctx, res, docsURL)
	default:
		return u.updateDirect(ctx, client, res, rel, docsURL)
	}
}

func (u *Updater) planDryRun(res *Result, method Method, rel *Release, docsURL string) (*Result, error) {
	switch method {
	case MethodHomebrew:
		cmd := []string{"brew", "upgrade", HomebrewFormula}
		if res.UpToDate {
			cmd = []string{"brew", "reinstall", HomebrewFormula}
		}
		res.Action = "homebrew_upgrade"
		res.PlannedCommand = cmd
		res.Message = fmt.Sprintf("Would run: %s", strings.Join(cmd, " "))
		return res, nil
	case MethodGoInstall:
		cmd := []string{"go", "install", GoInstallPkg + "@" + res.TargetVersion}
		res.Action = "go_install"
		res.PlannedCommand = cmd
		res.Message = fmt.Sprintf("Would run: %s", strings.Join(cmd, " "))
		return res, nil
	default:
		archiveName, ok := ArchiveName(rel.TagName, u.cfg.GOOS, u.cfg.GOARCH)
		if !ok {
			return nil, withGuidance(
				contract.Fail(contract.KindNotFound, contract.ExitNotFound, "No prebuilt cchp release archive is available for %s/%s.", u.cfg.GOOS, u.cfg.GOARCH),
				res.InstallMethod, res.TargetVersion, docsURL, u.cfg.GOOS, "",
			)
		}
		archiveAsset, hasArchive := rel.FindAsset(archiveName)
		if !hasArchive {
			return nil, withGuidance(
				contract.Fail(contract.KindNotFound, contract.ExitNotFound, "Release %s does not include archive %q.", rel.TagName, archiveName),
				res.InstallMethod, res.TargetVersion, docsURL, u.cfg.GOOS, "",
			)
		}
		checksumAsset, hasChecksums := rel.FindAsset("checksums.txt")
		if !hasChecksums {
			return nil, withGuidance(
				contract.Fail(contract.KindServer, contract.ExitServer, "Release %s is missing checksums.txt; refusing to install unverified archive.", rel.TagName),
				res.InstallMethod, res.TargetVersion, docsURL, u.cfg.GOOS, "",
			)
		}
		res.Action = "download_and_replace"
		res.ArchiveName = archiveName
		res.DownloadURL = archiveAsset.BrowserDownloadURL
		res.ChecksumsURL = checksumAsset.BrowserDownloadURL
		res.Message = fmt.Sprintf("Would download %s, verify SHA-256, and replace %s.", archiveName, res.ExecutablePath)
		return res, nil
	}
}

func (u *Updater) updateHomebrew(ctx context.Context, res *Result, force bool, docsURL string) (*Result, error) {
	if _, err := u.cfg.LookPath("brew"); err != nil {
		return nil, withGuidance(
			contract.Fail(contract.KindNotFound, contract.ExitNotFound, "Detected a Homebrew installation, but `brew` was not found in PATH."),
			MethodHomebrew, res.TargetVersion, docsURL, u.cfg.GOOS, "",
		)
	}

	if force && res.UpToDate {
		u.progress("Reinstalling cchp via Homebrew…")
		cmd := []string{"brew", "reinstall", HomebrewFormula}
		_, stderr, err := u.runCmd(ctx, cmd[0], cmd[1:]...)
		if err != nil {
			return nil, withGuidance(cmdFailure(cmd, stderr, err), MethodHomebrew, res.TargetVersion, docsURL, u.cfg.GOOS, "")
		}
		res.Action = "homebrew_reinstall"
		res.Command = cmd
		res.Updated = true
		res.Message = fmt.Sprintf("Reinstalled cchp %s via Homebrew.", res.TargetVersion)
		return res, nil
	}

	u.progress("Upgrading cchp via Homebrew…")
	upgradeCmd := []string{"brew", "upgrade", HomebrewFormula}
	stdout, stderr, err := u.runCmd(ctx, upgradeCmd[0], upgradeCmd[1:]...)
	combined := strings.ToLower(stdout + "\n" + stderr)
	alreadyInstalled := isHomebrewAlreadyUpToDate(combined)

	if err != nil && !alreadyInstalled {
		return nil, withGuidance(cmdFailure(upgradeCmd, stderr, err), MethodHomebrew, res.TargetVersion, docsURL, u.cfg.GOOS, "")
	}

	// If Homebrew reports already installed while GitHub has a newer release,
	// refresh the tap index with `brew update` and try upgrading once more.
	if alreadyInstalled && res.UpdateAvailable {
		u.progress("Refreshing Homebrew formulas (`brew update`)…")
		updateCmd := []string{"brew", "update"}
		if _, uStderr, uErr := u.runCmd(ctx, updateCmd[0], updateCmd[1:]...); uErr != nil {
			return nil, withGuidance(cmdFailure(updateCmd, uStderr, uErr), MethodHomebrew, res.TargetVersion, docsURL, u.cfg.GOOS, "")
		}
		u.progress("Retrying Homebrew upgrade…")
		stdout2, stderr2, err2 := u.runCmd(ctx, upgradeCmd[0], upgradeCmd[1:]...)
		combined2 := strings.ToLower(stdout2 + "\n" + stderr2)
		already2 := isHomebrewAlreadyUpToDate(combined2)
		if err2 != nil && !already2 {
			return nil, withGuidance(cmdFailure(upgradeCmd, stderr2, err2), MethodHomebrew, res.TargetVersion, docsURL, u.cfg.GOOS, "")
		}
		alreadyInstalled = already2
	}

	if alreadyInstalled && force {
		u.progress("Reinstalling cchp via Homebrew…")
		reinstallCmd := []string{"brew", "reinstall", HomebrewFormula}
		if _, rStderr, rErr := u.runCmd(ctx, reinstallCmd[0], reinstallCmd[1:]...); rErr != nil {
			return nil, withGuidance(cmdFailure(reinstallCmd, rStderr, rErr), MethodHomebrew, res.TargetVersion, docsURL, u.cfg.GOOS, "")
		}
		res.Action = "homebrew_reinstall"
		res.Command = reinstallCmd
		res.Updated = true
		res.Message = fmt.Sprintf("Reinstalled cchp via Homebrew (%s).", res.TargetVersion)
		return res, nil
	}

	if alreadyInstalled {
		res.Action = "homebrew_upgrade"
		res.Command = upgradeCmd
		res.Updated = false
		res.UpToDate = true
		if res.UpdateAvailable {
			res.Message = fmt.Sprintf("Homebrew already has the latest formula installed; %s may not be published to cch-hq/tap yet.", res.TargetVersion)
		} else {
			res.Message = fmt.Sprintf("cchp %s is already up to date in Homebrew.", res.CurrentVersion)
		}
		return res, nil
	}

	res.Action = "homebrew_upgrade"
	res.Command = upgradeCmd
	res.Updated = true
	res.UpToDate = false
	res.Message = fmt.Sprintf("Updated cchp %s -> %s via Homebrew.", res.CurrentVersion, res.TargetVersion)
	return res, nil
}

func isHomebrewAlreadyUpToDate(lowerOutput string) bool {
	return strings.Contains(lowerOutput, "already installed") ||
		strings.Contains(lowerOutput, "already up-to-date") ||
		strings.Contains(lowerOutput, "already up to date")
}

func (u *Updater) updateGoInstall(ctx context.Context, res *Result, docsURL string) (*Result, error) {
	if _, err := u.cfg.LookPath("go"); err != nil {
		return nil, withGuidance(
			contract.Fail(contract.KindNotFound, contract.ExitNotFound, "Detected a Go installation, but `go` was not found in PATH."),
			MethodGoInstall, res.TargetVersion, docsURL, u.cfg.GOOS, "",
		)
	}

	cmd := []string{"go", "install", GoInstallPkg + "@" + res.TargetVersion}
	u.progress(fmt.Sprintf("Updating cchp via `%s`…", strings.Join(cmd, " ")))
	_, stderr, err := u.runCmd(ctx, cmd[0], cmd[1:]...)
	if err != nil {
		return nil, withGuidance(cmdFailure(cmd, stderr, err), MethodGoInstall, res.TargetVersion, docsURL, u.cfg.GOOS, "")
	}

	res.Action = "go_install"
	res.Command = cmd
	res.Updated = true
	res.UpToDate = false
	res.Message = fmt.Sprintf("Updated cchp %s -> %s via go install.", res.CurrentVersion, res.TargetVersion)
	return res, nil
}

func (u *Updater) updateDirect(ctx context.Context, client *releaseClient, res *Result, rel *Release, docsURL string) (*Result, error) {
	fail := func(f *contract.Failure) error {
		return withGuidance(f, MethodDirect, res.TargetVersion, docsURL, u.cfg.GOOS, "")
	}

	archiveName, ok := ArchiveName(rel.TagName, u.cfg.GOOS, u.cfg.GOARCH)
	if !ok {
		return nil, fail(contract.Fail(contract.KindNotFound, contract.ExitNotFound, "No prebuilt cchp release archive is available for %s/%s.", u.cfg.GOOS, u.cfg.GOARCH))
	}
	archiveAsset, hasArchive := rel.FindAsset(archiveName)
	if !hasArchive {
		return nil, fail(contract.Fail(contract.KindNotFound, contract.ExitNotFound, "Release %s does not include archive %q.", rel.TagName, archiveName))
	}
	checksumAsset, hasChecksums := rel.FindAsset("checksums.txt")
	if !hasChecksums {
		return nil, fail(contract.Fail(contract.KindServer, contract.ExitServer, "Release %s is missing checksums.txt; refusing to install an unverified archive.", rel.TagName))
	}

	res.ArchiveName = archiveName
	res.DownloadURL = archiveAsset.BrowserDownloadURL
	res.ChecksumsURL = checksumAsset.BrowserDownloadURL

	workDir, err := os.MkdirTemp("", "cchp-update-*")
	if err != nil {
		return nil, fail(contract.Fail(contract.KindInternal, contract.ExitInternal, "Creating temporary directory for update: %v", err))
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	u.progress("Downloading checksums.txt…")
	checksumsData, err := client.getBytes(ctx, checksumAsset.BrowserDownloadURL, "text/plain, */*", maxChecksumBytes)
	if err != nil {
		return nil, fail(asFailure(err))
	}
	checksums, err := ParseChecksums(checksumsData)
	if err != nil {
		return nil, fail(contract.Fail(contract.KindServer, contract.ExitServer, "Invalid checksums.txt in release %s: %v", rel.TagName, err))
	}
	expectedSum, ok := checksums[archiveName]
	if !ok {
		return nil, fail(contract.Fail(contract.KindServer, contract.ExitServer, "checksums.txt in release %s has no entry for %q; refusing to install.", rel.TagName, archiveName))
	}

	archivePath := filepath.Join(workDir, archiveName)
	u.progress(fmt.Sprintf("Downloading %s…", archiveName))
	if _, err := client.downloadFile(ctx, archiveAsset.BrowserDownloadURL, archivePath, maxArchiveBytes); err != nil {
		return nil, fail(asFailure(err))
	}

	u.progress("Verifying SHA-256 checksum…")
	actualSum, err := VerifyFileSHA256(archivePath, expectedSum)
	if err != nil {
		return nil, fail(contract.Fail(contract.KindServer, contract.ExitServer, "Checksum verification failed for %s: %v", archiveName, err))
	}
	res.SHA256 = actualSum

	binaryName := BinaryName(u.cfg.GOOS)
	extractedPath := filepath.Join(workDir, binaryName)
	u.progress(fmt.Sprintf("Extracting %s…", binaryName))
	if err := ExtractBinary(archivePath, binaryName, extractedPath); err != nil {
		return nil, fail(contract.Fail(contract.KindServer, contract.ExitServer, "Extracting %s from %s failed: %v", binaryName, archiveName, err))
	}

	u.progress(fmt.Sprintf("Replacing %s…", res.ExecutablePath))
	if err := replaceBinary(res.ExecutablePath, extractedPath, u.cfg.GOOS == "windows"); err != nil {
		return nil, fail(asFailure(err))
	}

	res.Action = "download_and_replace"
	res.Updated = true
	res.UpToDate = false
	res.Message = fmt.Sprintf("Updated cchp %s -> %s (%s).", res.CurrentVersion, res.TargetVersion, res.ExecutablePath)
	return res, nil
}

func (u *Updater) runCmd(ctx context.Context, name string, args ...string) (string, string, error) {
	if u.cfg.Debug != nil {
		fmt.Fprintf(u.cfg.Debug, "debug: exec %s %s\n", name, strings.Join(args, " "))
	}
	return u.cfg.CmdRunner.Run(ctx, name, args...)
}

func (u *Updater) progress(msg string) {
	if u.cfg.OnProgress != nil {
		u.cfg.OnProgress(msg)
	}
}

func cmdFailure(cmd []string, stderr string, err error) *contract.Failure {
	if errors.Is(err, context.DeadlineExceeded) {
		f := contract.Fail(contract.KindUnknownOutcome, contract.ExitUnknown, "Command `%s` timed out before --timeout ran out.", strings.Join(cmd, " "))
		f.Err.Retryable = true
		return f
	}
	f := contract.Fail(contract.KindInternal, contract.ExitInternal, "Command `%s` failed: %v", strings.Join(cmd, " "), err)
	if trimmed := strings.TrimSpace(stderr); trimmed != "" {
		detail := redact.String(truncateText(trimmed, 400))
		f.Err.Detail = detail
		firstLine, _, _ := strings.Cut(detail, "\n")
		if firstLine = strings.TrimSpace(firstLine); firstLine != "" {
			f.Err.Message = fmt.Sprintf("Command `%s` failed: %s", strings.Join(cmd, " "), firstLine)
		}
	}
	return f
}

func asFailure(err error) *contract.Failure {
	if f, ok := errors.AsType[*contract.Failure](err); ok {
		return f
	}
	return contract.Fail(contract.KindInternal, contract.ExitInternal, "%v", err)
}
