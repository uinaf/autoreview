package provider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestRunProcessBoundsOutput(t *testing.T) {
	t.Parallel()

	script := writeTestExecutable(t, "flood", "#!/bin/sh\nprintf '0123456789abcdef'\n")
	result, err := runProcess(context.Background(), processSpec{
		Path:        script,
		Directory:   t.TempDir(),
		Environment: []string{"PATH=/usr/bin:/bin"},
		Timeout:     time.Second,
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

func TestRunProcessTimeoutKillsChildProcessGroup(t *testing.T) {
	t.Parallel()

	marker := filepath.Join(t.TempDir(), "survived")
	script := writeTestExecutable(t, "child", "#!/bin/sh\n(sleep 0.4; printf survived > "+shellQuote(marker)+") &\nwait\n")
	started := time.Now()
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
	if time.Since(started) > 3*time.Second {
		t.Fatalf("timeout cleanup took %s", time.Since(started))
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("child process survived cancellation: %v", err)
	}
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
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
