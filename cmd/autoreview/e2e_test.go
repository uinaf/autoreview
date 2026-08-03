package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/uinaf/autoreview/internal/protocol"
)

const providerOutputSentinel = "AR_REVIEW_SOURCE_SENTINEL_7f8e9d"

func TestBinaryEndToEndWithFakeCodex(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "autoreview")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".")
	build.Dir = workingDirectory
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v: %s", err, output)
	}

	tests := []struct {
		name         string
		scenario     string
		retries      string
		interrupt    bool
		wantExit     int
		wantStatus   protocol.Status
		wantFailure  protocol.FailureClass
		wantAttempts int
		wantCalls    string
		output       string
	}{
		{name: "clean", scenario: "clean", retries: "0", wantExit: 0, wantStatus: protocol.StatusClean, wantAttempts: 1, wantCalls: "1"},
		{name: "findings", scenario: "findings", retries: "0", wantExit: 1, wantStatus: protocol.StatusFindings, wantAttempts: 1, wantCalls: "1"},
		{name: "malformed retry", scenario: "retry", retries: "1", wantExit: 0, wantStatus: protocol.StatusClean, wantAttempts: 2, wantCalls: "2"},
		{name: "provider failure", scenario: "failure", retries: "1", wantExit: 2, wantStatus: protocol.StatusFailure, wantFailure: protocol.FailureProvider, wantAttempts: 1, wantCalls: "1"},
		{name: "provider failure terminal", scenario: "failure", retries: "1", output: "terminal", wantExit: 2, wantStatus: protocol.StatusFailure, wantFailure: protocol.FailureProvider, wantAttempts: 1, wantCalls: "1"},
		{name: "interrupt", scenario: "delay", retries: "1", interrupt: true, wantExit: 2, wantStatus: protocol.StatusFailure, wantFailure: protocol.FailureCancelled, wantAttempts: 1, wantCalls: "1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.output == "" {
				test.output = "json"
			}
			repository := reviewRepository(t)
			toolsDirectory, calls, providerPID := writeFakeReviewTools(t, test.scenario)
			xdg := t.TempDir()
			arguments := []string{
				"review", "--repository", repository, "--mode", "local", "--engine", "codex",
				"--retries", test.retries, "--timeout", "8s", "--output", test.output,
			}
			command := exec.Command(binary, arguments...)
			command.Env = replaceEnvironment(os.Environ(), map[string]string{
				"PATH":            toolsDirectory + ":/usr/bin:/bin",
				"OPENAI_API_KEY":  "fake-provider-credential",
				"XDG_CONFIG_HOME": xdg,
			})
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr

			var runErr error
			if test.interrupt {
				if err := command.Start(); err != nil {
					t.Fatal(err)
				}
				waitForFile(t, calls, 5*time.Second)
				if err := command.Process.Signal(os.Interrupt); err != nil {
					t.Fatal(err)
				}
				finished := make(chan error, 1)
				go func() { finished <- command.Wait() }()
				select {
				case runErr = <-finished:
				case <-time.After(2 * time.Second):
					_ = command.Process.Kill()
					t.Fatal("interrupted CLI did not terminate its provider")
				}
			} else {
				runErr = command.Run()
			}
			if exitCode(runErr) != test.wantExit {
				t.Fatalf("exit=%d want=%d err=%v stdout=%s stderr=%s", exitCode(runErr), test.wantExit, runErr, stdout.String(), stderr.String())
			}
			if test.output == "json" {
				var result protocol.Report
				if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
					t.Fatalf("decode stdout: %v: %s", err, stdout.String())
				}
				if err := result.Validate(); err != nil {
					t.Fatalf("validate report: %v", err)
				}
				if result.Status != test.wantStatus || len(result.Metadata.Attempts) != test.wantAttempts {
					t.Fatalf("status=%s attempts=%+v", result.Status, result.Metadata.Attempts)
				}
				if test.wantFailure != "" && (result.Failure == nil || result.Failure.Class != test.wantFailure) {
					t.Fatalf("failure=%+v", result.Failure)
				}
				if result.Metadata.ProtocolRecovery.Applied {
					t.Fatalf("orchestration retry was misreported as provider-local recovery: %+v", result.Metadata.ProtocolRecovery)
				}
			} else if !strings.Contains(stdout.String(), "status: "+string(test.wantStatus)) || !strings.Contains(stdout.String(), "failure: "+string(test.wantFailure)+":") {
				t.Fatalf("terminal report = %q", stdout.String())
			}
			if strings.Contains(stdout.String(), providerOutputSentinel) || strings.Contains(stderr.String(), providerOutputSentinel) {
				t.Fatalf("provider output escaped into CLI report: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			callCount, err := os.ReadFile(calls)
			if err != nil || strings.TrimSpace(string(callCount)) != test.wantCalls {
				t.Fatalf("provider calls=%q err=%v want=%s", callCount, err, test.wantCalls)
			}
			if strings.Contains(stderr.String(), `"schema_version"`) || !strings.Contains(stderr.String(), "reviewing with codex") {
				t.Fatalf("stdout/stderr contract failed: stderr=%q", stderr.String())
			}
			if test.interrupt {
				assertProcessExited(t, providerPID)
			}
		})
	}
}

func writeFakeReviewTools(t *testing.T, scenario string) (string, string, string) {
	t.Helper()
	directory := t.TempDir()
	calls := filepath.Join(directory, "calls")
	providerPID := filepath.Join(directory, "provider-pid")
	clean := `{"findings":[],"overall_explanation":"No defects.","overall_confidence":0.95}`
	findings := `{"findings":[{"title":"Defect","body":"Broken behavior.","priority":"P1","confidence":0.9,"category":"bug","location":{"file_path":"app.go","start_line":1,"end_line":1}}],"overall_explanation":"One defect.","overall_confidence":0.9}`
	result := clean
	if scenario == "findings" {
		result = findings
	}
	envelope := fakeCodexEnvelope(t, result)
	malformedEnvelope := fakeCodexEnvelope(t, "not-json")
	script := "#!/bin/sh\nset -eu\n" +
		"if [ \"${1:-}\" = \"--version\" ]; then printf '%s\\n' 'codex-cli 0.146.0'; exit 0; fi\n" +
		"if [ \"${1:-}\" = \"--help\" ]; then printf '%s\\n' '--ask-for-approval --strict-config --search'; exit 0; fi\n" +
		"if [ \"${1:-}\" = \"exec\" ] && [ \"${2:-}\" = \"--help\" ]; then printf '%s\\n' '--ephemeral --skip-git-repo-check --output-schema --output-last-message --json --cd --ignore-user-config --ignore-rules --sandbox'; exit 0; fi\n" +
		"output=''\nprevious=''\nfor argument in \"$@\"; do\n  if [ \"$previous\" = \"--output-last-message\" ]; then output=\"$argument\"; fi\n  previous=\"$argument\"\ndone\n" +
		"test -n \"$output\"\n/bin/cat >/dev/null\n" +
		"printf '%s\\n' \"$$\" > " + shellLiteral(providerPID) + "\n" +
		"count=0\nif [ -f " + shellLiteral(calls) + " ]; then count=$(/bin/cat " + shellLiteral(calls) + "); fi\ncount=$((count + 1))\nprintf '%s\\n' \"$count\" > " + shellLiteral(calls) + "\n" +
		"result=" + shellLiteral(result) + "\nenvelope=" + shellLiteral(envelope) + "\n"
	if scenario == "retry" {
		script += "if [ \"$count\" -eq 1 ]; then result='not-json'; envelope=" + shellLiteral(malformedEnvelope) + "; fi\n"
	}
	if scenario == "failure" {
		script += "printf '%s\\n' '" + providerOutputSentinel + "' >&2\nexit 7\n"
	}
	if scenario == "delay" {
		script += "/bin/sleep 30\n"
	}
	script += "printf '%s' \"$result\" > \"$output\"\nprintf '%s' \"$envelope\"\n"
	if err := os.WriteFile(filepath.Join(directory, "codex"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "trufflehog"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return directory, calls, providerPID
}

func fakeCodexEnvelope(t *testing.T, message string) string {
	t.Helper()
	item, err := json.Marshal(map[string]any{
		"type": "item.completed",
		"item": map[string]any{"id": "item_0", "type": "agent_message", "text": message},
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join([]string{
		`{"type":"thread.started","thread_id":"fake-thread"}`,
		`{"type":"turn.started"}`,
		string(item),
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
		"",
	}, "\n")
}

func shellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func replaceEnvironment(current []string, replacements map[string]string) []string {
	result := make([]string, 0, len(current)+len(replacements))
	for _, entry := range current {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := replacements[name]; !replaced {
			result = append(result, entry)
		}
	}
	for name, value := range replacements {
		result = append(result, name+"="+value)
	}
	return result
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func assertProcessExited(t *testing.T, pidPath string) {
	t.Helper()
	content, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("provider process %d survived cancellation: %v", pid, err)
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}
