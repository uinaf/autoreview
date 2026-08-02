package target

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type truffleHogScanner struct {
	path string
}

func newTruffleHogScanner(path string) (*truffleHogScanner, error) {
	if path == "" {
		var err error
		path, err = exec.LookPath("trufflehog")
		if err != nil {
			return nil, fmt.Errorf("find trufflehog: %w", err)
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve trufflehog path: %w", err)
	}
	return &truffleHogScanner{path: absolute}, nil
}

func (scanner *truffleHogScanner) Scan(ctx context.Context, payload []byte) error {
	directory, err := os.MkdirTemp("", "autoreview-scan-")
	if err != nil {
		return fmt.Errorf("create secret-scan directory: %w", err)
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "frozen-review.txt")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("write secret-scan input: %w", err)
	}

	command := exec.CommandContext(ctx, scanner.path,
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
	)
	command.Env = hardenedScannerEnvironment(directory)
	command.WaitDelay = 2 * time.Second
	stdout := newLimitBuffer(1 << 20)
	stderr := newLimitBuffer(diagnosticLimit)
	command.Stdout = stdout
	command.Stderr = stderr
	err = command.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if stdout.exceeded {
		return fmt.Errorf("trufflehog output exceeded safe diagnostic limit")
	}
	if stderr.exceeded {
		return fmt.Errorf("trufflehog diagnostic output exceeded safe limit")
	}
	if len(bytes.TrimSpace(stdout.Bytes())) > 0 {
		return ErrSecretFound
	}
	if err != nil {
		return fmt.Errorf("trufflehog scan failed: %s", sanitizeDiagnostic(strings.TrimSpace(stderr.String())))
	}
	return nil
}

func hardenedScannerEnvironment(home string) []string {
	return []string{"HOME=" + home, "LANG=C", "LC_ALL=C", "TRUFFLEHOG_NO_UPDATE=true"}
}
