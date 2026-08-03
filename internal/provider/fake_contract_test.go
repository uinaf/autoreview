package provider

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uinaf/autoreview/internal/protocol"
	contractschema "github.com/uinaf/autoreview/schema"
)

func TestFakeProviderCLIsRejectInvalidReviewArguments(t *testing.T) {
	t.Parallel()

	t.Run("codex", func(t *testing.T) {
		t.Parallel()
		workspace := t.TempDir()
		schemaPath := filepath.Join(workspace, "review.schema.json")
		outputPath := filepath.Join(workspace, "review.json")
		if err := os.WriteFile(schemaPath, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(outputPath, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		valid := codexArguments(codexConfig(protocol.IsolationStrict, false, 5*time.Second), workspace, schemaPath, outputPath, "test-model")
		assertInvalidContracts(t, newFakeCodex(t, fakeCodexOptions{}).path, valid, []string{"--search"})
	})

	t.Run("claude", func(t *testing.T) {
		t.Parallel()
		schema, err := contractschema.ClaudeReviewV1()
		if err != nil {
			t.Fatal(err)
		}
		valid := claudeArguments(claudeConfig(protocol.IsolationStrict, false, 5*time.Second), string(schema), "test-model")
		assertInvalidContracts(t, newFakeClaude(t, fakeClaudeOptions{}).path, valid, []string{"--allowedTools", "WebSearch"})
		t.Run("missing tools value", func(t *testing.T) {
			toolsIndex := indexOf(valid, "--tools")
			if toolsIndex < 0 {
				t.Fatal("valid fixture arguments are missing --tools")
			}
			expectContractFailure(t, newFakeClaude(t, fakeClaudeOptions{}).path, append([]string(nil), valid[:toolsIndex+1]...), "review input")
		})
	})

	t.Run("cursor", func(t *testing.T) {
		t.Parallel()
		workspace := t.TempDir()
		valid := cursorArguments(cursorConfig(protocol.IsolationStrict, true, 5*time.Second), workspace, "test-model")
		assertInvalidContracts(t, newFakeCursor(t, fakeCursorOptions{}).path, valid, []string{"--sandbox", "disabled"})
	})
}

func TestFakeProviderCLIsRejectInvalidProbeArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		executable func(*testing.T) string
		arguments  []string
	}{
		{name: "codex extra version flag", executable: func(t *testing.T) string { return newFakeCodex(t, fakeCodexOptions{}).path }, arguments: []string{"--version", "--json"}},
		{name: "claude incomplete auth", executable: func(t *testing.T) string { return newFakeClaude(t, fakeClaudeOptions{}).path }, arguments: []string{"auth", "status"}},
		{name: "cursor conflicting status format", executable: func(t *testing.T) string { return newFakeCursor(t, fakeCursorOptions{}).path }, arguments: []string{"status", "--format", "text"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			expectContractFailure(t, test.executable(t), test.arguments, "")
		})
	}
}

func assertInvalidContracts(t *testing.T, executable string, valid, conflict []string) {
	t.Helper()
	mutations := []struct {
		name      string
		arguments []string
	}{
		{name: "missing prompt", arguments: append([]string(nil), valid...)},
		{name: "unknown flag", arguments: appendCopy(valid, "--unknown")},
		{name: "duplicate flag", arguments: appendCopy(valid, "--model", "duplicate")},
		{name: "conflicting flag", arguments: appendCopy(valid, conflict...)},
		{name: "misplaced value", arguments: swapFirstTwo(valid)},
		{name: "flag-like model value", arguments: replaceArgumentAfter(t, valid, "--model", "--other-flag")},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			input := "review input"
			if mutation.name == "missing prompt" {
				input = ""
			}
			expectContractFailure(t, executable, mutation.arguments, input)
		})
	}
}

func expectContractFailure(t *testing.T, executable string, arguments []string, input string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Stdin = strings.NewReader(input)
	output, err := command.CombinedOutput()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 64 {
		t.Fatalf("contract result = %v, output = %q, want exit 64", err, output)
	}
}

func appendCopy(values []string, suffix ...string) []string {
	copyOfValues := append([]string(nil), values...)
	return append(copyOfValues, suffix...)
}

func swapFirstTwo(values []string) []string {
	copyOfValues := append([]string(nil), values...)
	copyOfValues[0], copyOfValues[1] = copyOfValues[1], copyOfValues[0]
	return copyOfValues
}

func replaceArgumentAfter(t *testing.T, values []string, flag, replacement string) []string {
	t.Helper()
	copyOfValues := append([]string(nil), values...)
	index := indexOf(copyOfValues, flag)
	if index < 0 || index+1 >= len(copyOfValues) {
		t.Fatalf("missing value after %q in valid fixture arguments", flag)
	}
	copyOfValues[index+1] = replacement
	return copyOfValues
}
