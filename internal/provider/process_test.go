package provider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"
)

const providerOutputSentinel = "AR_REVIEW_SOURCE_SENTINEL_7f8e9d"

func TestRunProcessBoundsOutput(t *testing.T) {
	t.Parallel()

	marker := filepath.Join(t.TempDir(), "survived")
	script := writeTestExecutable(t, "flood", "#!/bin/sh\n(/bin/sleep 0.25; printf survived > "+shellQuote(marker)+") &\nprintf '0123456789abcdef'\n")
	result, err := runProcess(context.Background(), processSpec{
		Path:        script,
		Directory:   t.TempDir(),
		Environment: []string{"PATH=/usr/bin:/bin"},
		Timeout:     5 * time.Second,
		StdoutLimit: 8,
		StderrLimit: 8,
	})
	var failure *processError
	if !errors.As(err, &failure) || failure.Kind != processOutputLimit {
		t.Fatalf("runProcess() error = %v", err)
	}
	if string(result.Stdout) != "01234567" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
	assertMarkerNotWritten(t, marker)
}

func TestRunProcessRetriesBusyExecutable(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("ETXTBSY executable publication behavior is Linux-specific")
	}

	script := writeTestExecutable(t, "busy", "#!/bin/sh\nprintf 'ready\\n'\n")
	writer, err := os.OpenFile(script, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	time.AfterFunc(35*time.Millisecond, func() {
		closed <- writer.Close()
	})

	result, runErr := runProcess(context.Background(), processSpec{
		Path:        script,
		Environment: []string{"PATH=/usr/bin:/bin"},
		Timeout:     2 * time.Second,
		StdoutLimit: 64,
		StderrLimit: 64,
	})
	if closeErr := <-closed; closeErr != nil {
		t.Fatal(closeErr)
	}
	if runErr != nil {
		t.Fatalf("runProcess() error = %v", runErr)
	}
	if string(result.Stdout) != "ready\n" {
		t.Fatalf("stdout = %q, want %q", result.Stdout, "ready\\n")
	}
}

func TestRunProcessRejectsNegativeOutputLimits(t *testing.T) {
	t.Parallel()

	_, err := runProcess(context.Background(), processSpec{Timeout: time.Second, StdoutLimit: -1})
	var failure *processError
	if !errors.As(err, &failure) || failure.Kind != processStart || !strings.Contains(err.Error(), "output limits") {
		t.Fatalf("runProcess() error = %v", err)
	}
}

func TestRunProcessEmptyEnvironmentDoesNotInheritParent(t *testing.T) {
	t.Parallel()

	script := writeTestExecutable(t, "environment", "#!/bin/sh\n/usr/bin/env\n")
	result, err := runProcess(context.Background(), processSpec{
		Path: script, Directory: t.TempDir(), Environment: []string{}, Timeout: 5 * time.Second, StdoutLimit: 1024, StderrLimit: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(result.Stdout), "PATH=") || strings.Contains(string(result.Stdout), "HOME=") {
		t.Fatalf("child inherited environment: %q", result.Stdout)
	}
}

func TestReadProviderResultUsesOriginalDescriptor(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "result.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if _, err := file.WriteString("original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, err := readProviderResult(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Fatalf("readProviderResult() = %q", content)
	}
}

func TestRunProcessCancellationKillsChildProcessGroup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "survived")
	childPID := filepath.Join(t.TempDir(), "child.pid")
	script := writeTestExecutable(t, "child", "#!/bin/sh\n(sleep 10; printf survived > "+shellQuote(marker)+") &\nprintf '%s' \"$!\" > "+shellQuote(childPID)+"\nwait\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type readyResult struct {
		pid string
		err error
	}
	ready := make(chan readyResult, 1)
	go func() {
		deadline := time.Now().Add(3 * time.Second)
		for {
			content, err := os.ReadFile(childPID)
			if err == nil && len(content) != 0 {
				ready <- readyResult{pid: string(content)}
				cancel()
				return
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				ready <- readyResult{err: err}
				cancel()
				return
			}
			if time.Now().After(deadline) {
				ready <- readyResult{err: errors.New("child did not become ready")}
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	started := time.Now()
	_, err := runProcess(ctx, processSpec{
		Path:        script,
		Directory:   t.TempDir(),
		Environment: []string{"PATH=/usr/bin:/bin"},
		Timeout:     5 * time.Second,
		StdoutLimit: 1024,
		StderrLimit: 1024,
	})
	var failure *processError
	if !errors.As(err, &failure) || failure.Kind != processCancelled {
		t.Fatalf("runProcess() error = %v", err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatalf("cancellation cleanup took %s", time.Since(started))
	}
	child := <-ready
	if child.err != nil {
		t.Fatal(child.err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(child.pid))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("child process %d survived cancellation", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("child process survived cancellation: %v", err)
	}
}

func TestRunProcessCleansDescendantsAfterLeaderExit(t *testing.T) {
	tests := []struct {
		name       string
		exitStatus int
	}{
		{name: "success"},
		{name: "failure", exitStatus: 7},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "survived")
			script := writeTestExecutable(t, "leader", "#!/bin/sh\n(/bin/sleep 0.25; printf survived > "+shellQuote(marker)+") &\nexit "+strconv.Itoa(test.exitStatus)+"\n")
			_, err := runProcess(context.Background(), processSpec{
				Path:        script,
				Directory:   t.TempDir(),
				Environment: []string{"PATH=/usr/bin:/bin"},
				Timeout:     5 * time.Second,
				StdoutLimit: 1024,
				StderrLimit: 1024,
			})
			if test.exitStatus == 0 {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var failure *processError
				if !errors.As(err, &failure) || failure.Kind != processExit || failure.Result.ExitCode != test.exitStatus {
					t.Fatalf("runProcess() error = %v", err)
				}
			}
			assertMarkerNotWritten(t, marker)
		})
	}
}

func TestRunProcessTimeoutKillsDescendants(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "survived")
	script := writeTestExecutable(t, "timeout", "#!/bin/sh\n(/bin/sleep 0.25; printf survived > "+shellQuote(marker)+") &\n/bin/sleep 10\n")
	_, err := runProcess(context.Background(), processSpec{
		Path:        script,
		Directory:   t.TempDir(),
		Environment: []string{"PATH=/usr/bin:/bin"},
		Timeout:     50 * time.Millisecond,
		StdoutLimit: 1024,
		StderrLimit: 1024,
	})
	var failure *processError
	if !errors.As(err, &failure) || failure.Kind != processTimeout {
		t.Fatalf("runProcess() error = %v", err)
	}
	assertMarkerNotWritten(t, marker)
}

func TestSanitizeDiagnosticRedactsSecretsAndEscapesControls(t *testing.T) {
	t.Parallel()

	value := sanitizeDiagnostic("\x1b[31mtoken-value\r", []string{"OPENAI_API_KEY=token-value"})
	if strings.Contains(value, "token-value") || strings.ContainsRune(value, '\x1b') || strings.ContainsRune(value, '\r') {
		t.Fatalf("sanitizeDiagnostic() = %q", value)
	}
	if !strings.Contains(value, "[redacted]") || !strings.Contains(value, `\x1b`) || !strings.Contains(value, `\x0d`) {
		t.Fatalf("sanitizeDiagnostic() = %q", value)
	}
}

func TestSanitizeDiagnosticEscapesUnicodeFormatCharacters(t *testing.T) {
	t.Parallel()

	value := sanitizeDiagnostic("safe\u202eevil\u200b", nil)
	if strings.ContainsRune(value, '\u202e') || strings.ContainsRune(value, '\u200b') || !strings.Contains(value, `\u202e`) || !strings.Contains(value, `\u200b`) {
		t.Fatalf("sanitizeDiagnostic() = %q", value)
	}
}

func TestSanitizeDiagnosticRedactsShortCredentialValues(t *testing.T) {
	t.Parallel()

	value := sanitizeDiagnostic("provider echoed abc E private-value pat-value database-value proxy-value auth-value cookie-value session-value secrets-value", []string{
		"TOKEN=abc",
		"TINY_TOKEN=E",
		"SSH_PRIVATE_KEY=private-value",
		"GITHUB_PAT=pat-value",
		"DATABASE_URL=database-value",
		"HTTPS_PROXY=proxy-value",
		"SERVICE_AUTHORIZATION=auth-value",
		"COOKIES=cookie-value",
		"SESSIONS=session-value",
		"MY_SECRETS=secrets-value",
	})
	for _, secret := range []string{
		"abc", " E ", "private-value", "pat-value", "database-value", "proxy-value",
		"auth-value", "cookie-value", "session-value", "secrets-value",
	} {
		if strings.Contains(value, secret) {
			t.Fatalf("sanitizeDiagnostic() retained %q: %q", secret, value)
		}
	}
	if strings.Count(value, "[redacted]") != 10 {
		t.Fatalf("sanitizeDiagnostic() = %q", value)
	}
}

func TestSanitizeDiagnosticRedactsInvalidUTF8Credential(t *testing.T) {
	t.Parallel()

	secret := "abc\xffdef"
	value := sanitizeDiagnostic("provider echoed "+secret, []string{"SECRET_VALUE=" + secret})
	if strings.Contains(value, "abc") || strings.Contains(value, "def") || !strings.Contains(value, "[redacted]") || !utf8.ValidString(value) {
		t.Fatalf("sanitizeDiagnostic() = %q", value)
	}
}

func writeTestExecutable(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	writeTestExecutableAt(t, path, content)
	return path
}

func writeTestExecutableAt(t *testing.T, path, content string) {
	t.Helper()
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		t.Fatal(err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.WriteString(content); err != nil {
		temporary.Close()
		t.Fatal(err)
	}
	if err := temporary.Chmod(0o700); err != nil {
		temporary.Close()
		t.Fatal(err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		t.Fatal(err)
	}
	if err := temporary.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		t.Fatal(err)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func assertMarkerNotWritten(t *testing.T, marker string) {
	t.Helper()
	time.Sleep(400 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant survived and wrote marker: %v", err)
	}
}
