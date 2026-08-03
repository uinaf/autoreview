package target

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/uinaf/autoreview/internal/processgroup"
	"github.com/uinaf/autoreview/internal/trustedexec"
)

type truffleHogScanner struct {
	path string
}

func newTruffleHogScanner(ctx context.Context, path, repository string) (scanner *truffleHogScanner, returnErr error) {
	root, err := os.MkdirTemp("", "autoreview-scan-probe-")
	if err != nil {
		return nil, fmt.Errorf("create trufflehog probe directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(root); err != nil {
			scanner = nil
			returnErr = errors.Join(returnErr, fmt.Errorf("remove trufflehog probe directory: %w", err))
		}
	}()
	home := filepath.Join(root, "home")
	input := filepath.Join(root, "input")
	for _, directory := range []string{home, input} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create trufflehog probe directory: %w", err)
		}
	}
	absolute, err := trustedexec.Resolve(
		ctx,
		"trufflehog",
		path,
		repository,
		os.Environ(),
		trustedexec.Probe(truffleHogArguments(input), root, hardenedScannerEnvironment(home)),
	)
	if err != nil {
		return nil, fmt.Errorf("find trufflehog: %w", err)
	}
	return &truffleHogScanner{path: absolute}, nil
}

func truffleHogArguments(directory string) []string {
	return []string{
		"filesystem",
		"--no-update",
		"--no-verification",
		"--fail-on-scan-errors",
		"--fail",
		"--json",
		"--no-color",
		"--log-level=-1",
		"--concurrency=1",
		directory,
	}
}

func (scanner *truffleHogScanner) Scan(ctx context.Context, payload string) (returnErr error) {
	root, err := os.MkdirTemp("", "autoreview-scan-")
	if err != nil {
		return fmt.Errorf("create secret-scan directory: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(root); removeErr != nil {
			cleanupErr := fmt.Errorf("remove secret-scan directory: %w", removeErr)
			if returnErr == nil {
				returnErr = cleanupErr
			} else {
				returnErr = errors.Join(returnErr, cleanupErr)
			}
		}
	}()
	directory := filepath.Join(root, "input")
	home := filepath.Join(root, "home")
	for _, path := range []string{directory, home} {
		if err := os.Mkdir(path, 0o700); err != nil {
			return fmt.Errorf("create secret-scan directory: %w", err)
		}
	}
	path := filepath.Join(directory, "frozen-review.txt")
	input, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create secret-scan input: %w", err)
	}
	_, writeErr := io.Copy(input, strings.NewReader(payload))
	closeErr := input.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write secret-scan input: %w", err)
	}

	//nolint:noctx // processgroup.Run owns cancellation so it can kill the group before reaping the leader.
	command := exec.Command(scanner.path, truffleHogArguments(directory)...)
	command.Dir = root
	command.Env = hardenedScannerEnvironment(home)
	stdout := newLimitBuffer(1 << 20)
	stderr := newLimitBuffer(diagnosticLimit)
	command.Stdout = stdout
	command.Stderr = stderr
	runResult := processgroup.Run(ctx, command)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return errors.Join(ctxErr, runResult.CleanupErr)
	}
	if stdout.exceeded {
		return errors.Join(fmt.Errorf("trufflehog output exceeded safe diagnostic limit"), runResult.CleanupErr)
	}
	if stderr.exceeded {
		return errors.Join(fmt.Errorf("trufflehog diagnostic output exceeded safe limit"), runResult.CleanupErr)
	}
	if len(bytes.TrimSpace(stdout.Bytes())) > 0 {
		return errors.Join(ErrSecretFound, runResult.CleanupErr)
	}
	if runResult.CommandErr != nil {
		diagnostic := sanitizeDiagnostic(strings.TrimSpace(stderr.String()))
		if diagnostic == "" {
			diagnostic = "no diagnostic output"
		}
		return errors.Join(fmt.Errorf("trufflehog scan failed: %s: %w", diagnostic, runResult.CommandErr), runResult.CleanupErr)
	}
	if runResult.CleanupErr != nil {
		return fmt.Errorf("trufflehog process cleanup failed: %w", runResult.CleanupErr)
	}
	return nil
}

func hardenedScannerEnvironment(home string) []string {
	return []string{"HOME=" + home, "LANG=C", "LC_ALL=C", "TRUFFLEHOG_NO_UPDATE=true"}
}
