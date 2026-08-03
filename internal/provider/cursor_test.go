package provider

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contractschema "github.com/uinaf/autoreview/schema"

	"github.com/uinaf/autoreview/internal/config"
	"github.com/uinaf/autoreview/internal/protocol"
	"github.com/uinaf/autoreview/internal/reviewpolicy"
)

func TestCursorReviewStrictUsesAPIKeyWithoutStatusAndDenyConfig(t *testing.T) {
	t.Parallel()

	fake := newFakeCursor(t, fakeCursorOptions{authError: "not authenticated"})
	reviewer := NewCursor(CursorOptions{
		Repository: t.TempDir(), Executable: fake.path,
		Environment: []string{
			"PATH=/usr/bin:/bin",
			"CURSOR_API_KEY=test-provider-secret",
			"OPENAI_API_KEY=must-not-pass",
			"HOME=/private/home",
		},
	})
	result, err := reviewer.Review(context.Background(), Request{Prompt: "frozen review bundle", Config: cursorConfig(protocol.IsolationStrict, true, 5*time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider.Name != protocol.ProviderCursor || result.Provider.Model != "test-model" || result.Provider.Version != "2026.07.23-e383d2b" {
		t.Fatalf("provider = %+v", result.Provider)
	}
	if result.Attempt.Outcome != protocol.AttemptValid || result.ProtocolRecovery.Applied || len(result.Review.Findings) != 0 {
		t.Fatalf("result = %+v", result)
	}
	prompt := readTestFile(t, fake.prompt)
	reviewProtocol := reviewpolicy.CursorReviewProtocol()
	schemaBoundary := "BEGIN AUTOREVIEW-TRUSTED-REVIEW-SCHEMA-V1\n" + string(contractschema.ReviewV1()) + "\nEND AUTOREVIEW-TRUSTED-REVIEW-SCHEMA-V1\n"
	if prompt != "frozen review bundle"+reviewProtocol ||
		!strings.HasPrefix(reviewProtocol, "\nAUTOREVIEW-TRUSTED-REVIEW-PROTOCOL-V1\n") ||
		!strings.Contains(reviewProtocol, schemaBoundary) ||
		!strings.HasSuffix(reviewProtocol, "Return only the review JSON object now.\n") {
		t.Fatalf("provider stdin omitted trusted review protocol: %q", prompt)
	}
	arguments := strings.Split(strings.TrimSpace(readTestFile(t, fake.arguments)), "\n")
	for _, required := range []string{"--print", "--output-format", "json", "--mode", "ask", "--sandbox", "enabled", "--workspace", "--trust", "--model", "test-model"} {
		if !contains(arguments, required) {
			t.Errorf("Cursor arguments omitted %q: %v", required, arguments)
		}
	}
	if argumentAfter(arguments, "--workspace") == "" {
		t.Fatalf("Cursor arguments = %v", arguments)
	}
	environment := readTestFile(t, fake.environment)
	if !strings.Contains(environment, "CURSOR_API_KEY=test-provider-secret") || strings.Contains(environment, "OPENAI_API_KEY") || strings.Contains(environment, "HOME=/private/home") {
		t.Fatalf("strict environment = %s", environment)
	}
	permissions := readTestFile(t, fake.permissions)
	var permissionDocument struct {
		Version     int `json:"version"`
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(permissions), &permissionDocument); err != nil {
		t.Fatalf("strict permissions are not JSON: %v: %s", err, permissions)
	}
	if permissionDocument.Version != 1 {
		t.Fatalf("strict permissions version = %d", permissionDocument.Version)
	}
	for _, denied := range []string{"Shell(*)", "Read(**)", "Read(/**)", "Write(**)", "Write(/**)", "Mcp(*)"} {
		if !contains(permissionDocument.Permissions.Deny, denied) {
			t.Errorf("strict permissions omitted %q: %s", denied, permissions)
		}
	}
}

func TestCursorReviewNativePreservesConfigurationAndOmitsForcedSandbox(t *testing.T) {
	t.Parallel()

	fake := newFakeCursor(t, fakeCursorOptions{})
	reviewer := NewCursor(CursorOptions{
		Repository: t.TempDir(), Executable: fake.path,
		Environment: []string{"PATH=/usr/bin:/bin", "HOME=/native/home", "CURSOR_CONFIG_DIR=/native/cursor"},
	})
	result, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: cursorConfig(protocol.IsolationNative, true, 5*time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if !result.WebAccess || result.Isolation != protocol.IsolationNative {
		t.Fatalf("result policy = %+v", result)
	}
	arguments := strings.Split(strings.TrimSpace(readTestFile(t, fake.arguments)), "\n")
	if contains(arguments, "--sandbox") || argumentAfter(arguments, "--mode") != "ask" {
		t.Fatalf("native arguments = %v", arguments)
	}
	environment := readTestFile(t, fake.environment)
	if !strings.Contains(environment, "HOME=/native/home") || !strings.Contains(environment, "CURSOR_CONFIG_DIR=/native/cursor") {
		t.Fatalf("native environment = %s", environment)
	}
	if _, err := os.Stat(fake.permissions); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("native mode generated strict permissions: %v", err)
	}
}

func TestCursorReviewRejectsUnsupportedWebPolicyBeforeDiscovery(t *testing.T) {
	t.Parallel()

	fake := newFakeCursor(t, fakeCursorOptions{})
	reviewer := NewCursor(CursorOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "CURSOR_API_KEY=secret"}})
	_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: cursorConfig(protocol.IsolationStrict, false, 5*time.Second)})
	failure := assertProviderError(t, err, protocol.FailureCapability)
	if !strings.Contains(failure.Message, "web_access=false") {
		t.Fatalf("failure = %q", failure.Message)
	}
	if _, err := os.Stat(fake.arguments); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Cursor was invoked despite unsupported web policy: %v", err)
	}
}

func TestCursorReviewRejectsCombinedPromptBeforeDiscovery(t *testing.T) {
	t.Parallel()

	protocolBytes := int64(len(reviewpolicy.CursorReviewProtocol()))
	effective := cursorConfig(protocol.IsolationStrict, true, 5*time.Second)
	effective.MaxBytes = config.Value[int64]{Value: protocolBytes, Source: config.SourceFlag}
	maximumPrompt := effective.MaxBytes.Value + providerPromptAllowance
	prompt := strings.Repeat("x", int(maximumPrompt-protocolBytes+1))
	reviewer := NewCursor(CursorOptions{Repository: t.TempDir(), Executable: "missing-cursor-agent"})
	_, err := reviewer.Review(context.Background(), Request{Prompt: prompt, Config: effective})
	failure := assertProviderError(t, err, protocol.FailureConfig)
	if !strings.Contains(failure.Message, "Cursor combined review input exceeds") {
		t.Fatalf("failure = %q", failure.Message)
	}
}

func TestCursorReviewRejectsSeparateReasoningConfiguration(t *testing.T) {
	t.Parallel()

	effective := cursorConfig(protocol.IsolationStrict, true, 5*time.Second)
	effective.ReasoningEffort.Source = config.SourceFlag
	reviewer := NewCursor(CursorOptions{Repository: t.TempDir()})
	_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: effective})
	_ = assertProviderError(t, err, protocol.FailureConfig)
}

func TestCursorReviewFailsCapabilityProbeBeforeInvocation(t *testing.T) {
	t.Parallel()

	fake := newFakeCursor(t, fakeCursorOptions{help: "--print --output-format status"})
	reviewer := NewCursor(CursorOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "CURSOR_API_KEY=secret"}})
	_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: cursorConfig(protocol.IsolationStrict, true, 5*time.Second)})
	_ = assertProviderError(t, err, protocol.FailureCapability)
	if _, err := os.Stat(fake.arguments); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("model was invoked after failed capability probe: %v", err)
	}
}

func TestCursorReviewRejectsMissingCapabilityValues(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		help string
	}{
		{name: "JSON output", help: cursorHelp("text | stream-json", "plan, ask", "enabled, disabled")},
		{name: "Ask mode", help: cursorHelp("text | json | stream-json", "plan", "enabled, disabled")},
		{name: "enabled sandbox", help: cursorHelp("text | json | stream-json", "plan, ask", "disabled")},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeCursor(t, fakeCursorOptions{help: test.help})
			reviewer := NewCursor(CursorOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "CURSOR_API_KEY=secret"}})
			_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: cursorConfig(protocol.IsolationStrict, true, 5*time.Second)})
			_ = assertProviderError(t, err, protocol.FailureCapability)
			if _, err := os.Stat(fake.arguments); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("model was invoked after failed capability value probe: %v", err)
			}
		})
	}
}

func TestCursorReviewReportsAuthenticationFailure(t *testing.T) {
	t.Parallel()

	fake := newFakeCursor(t, fakeCursorOptions{loggedOut: true})
	reviewer := NewCursor(CursorOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "HOME=/native/home"}})
	_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: cursorConfig(protocol.IsolationNative, true, 5*time.Second)})
	_ = assertProviderError(t, err, protocol.FailureAuth)
}

func TestCursorReviewRecordsTrailingObjectRecovery(t *testing.T) {
	t.Parallel()

	review := `{"findings":[],"overall_explanation":"No defects.","overall_confidence":0.95}`
	fake := newFakeCursor(t, fakeCursorOptions{output: cursorEnvelope("Here is the review.\n" + review)})
	reviewer := NewCursor(CursorOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "CURSOR_API_KEY=secret"}})
	result, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: cursorConfig(protocol.IsolationStrict, true, 5*time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ProtocolRecovery.Applied || result.ProtocolRecovery.Strategy == nil || *result.ProtocolRecovery.Strategy != protocol.RecoveryCursorTrailingObject {
		t.Fatalf("protocol recovery = %+v", result.ProtocolRecovery)
	}
}

func TestCursorReviewRejectsMalformedEnvelope(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		output string
	}{
		{name: "not JSON", output: "not-json"},
		{name: "failed result", output: `{"type":"result","subtype":"error","is_error":true,"result":"failed"}`},
		{name: "missing result", output: `{"type":"result","subtype":"success","is_error":false}`},
		{name: "duplicate key", output: `{"type":"result","type":"result","subtype":"success","is_error":false,"result":"x"}`},
		{name: "multiple envelopes", output: cursorEnvelope("x") + cursorEnvelope("x")},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeCursor(t, fakeCursorOptions{output: test.output})
			reviewer := NewCursor(CursorOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "CURSOR_API_KEY=secret"}})
			_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: cursorConfig(protocol.IsolationStrict, true, 5*time.Second)})
			failure := assertProviderError(t, err, protocol.FailureProtocol)
			if failure.Attempt == nil || failure.Attempt.Outcome != protocol.AttemptMalformed {
				t.Fatalf("attempt = %+v", failure.Attempt)
			}
		})
	}
}

func TestCursorReviewRedactsProviderFailure(t *testing.T) {
	t.Parallel()

	fake := newFakeCursor(t, fakeCursorOptions{reviewError: "\033[31mfailed with test-provider-secret\r"})
	reviewer := NewCursor(CursorOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "CURSOR_API_KEY=test-provider-secret"}})
	_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: cursorConfig(protocol.IsolationStrict, true, 5*time.Second)})
	failure := assertProviderError(t, err, protocol.FailureProvider)
	if strings.Contains(failure.Message, "test-provider-secret") || strings.ContainsRune(failure.Message, '\x1b') || !strings.Contains(failure.Message, "[redacted]") {
		t.Fatalf("provider failure = %q", failure.Message)
	}
}

func TestCursorReviewDistinguishesTimeoutAndCancellation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		timeout time.Duration
		cancel  bool
		class   protocol.FailureClass
	}{
		{name: "timeout", timeout: 100 * time.Millisecond, class: protocol.FailureTimeout},
		{name: "cancelled", timeout: 5 * time.Second, cancel: true, class: protocol.FailureCancelled},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeCursor(t, fakeCursorOptions{delay: "1"})
			reviewer := NewCursor(CursorOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "CURSOR_API_KEY=secret"}})
			ctx := context.Background()
			if test.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				t.Cleanup(cancel)
				go func() {
					time.Sleep(50 * time.Millisecond)
					cancel()
				}()
			}
			_, err := reviewer.Review(ctx, Request{Prompt: "bundle", Config: cursorConfig(protocol.IsolationStrict, true, test.timeout)})
			_ = assertProviderError(t, err, test.class)
		})
	}
}

func TestCursorReviewEnforcesOutputBounds(t *testing.T) {
	t.Parallel()

	fake := newFakeCursor(t, fakeCursorOptions{output: strings.Repeat("x", int(providerStdoutLimit)+1)})
	reviewer := NewCursor(CursorOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "CURSOR_API_KEY=secret"}})
	_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: cursorConfig(protocol.IsolationStrict, true, 5*time.Second)})
	failure := assertProviderError(t, err, protocol.FailureProvider)
	if failure.Attempt == nil || failure.Attempt.Outcome != protocol.AttemptFailed {
		t.Fatalf("attempt = %+v", failure.Attempt)
	}
}

func TestCursorReviewUsesExplicitDefaultModel(t *testing.T) {
	t.Parallel()

	effective := cursorConfig(protocol.IsolationStrict, true, 5*time.Second)
	effective.Model = config.Value[string]{Source: config.SourceDefault}
	fake := newFakeCursor(t, fakeCursorOptions{})
	reviewer := NewCursor(CursorOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "CURSOR_API_KEY=secret"}})
	result, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: effective})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider.Model != DefaultCursorModel {
		t.Fatalf("provider model = %q", result.Provider.Model)
	}
	arguments := strings.Split(strings.TrimSpace(readTestFile(t, fake.arguments)), "\n")
	if argumentAfter(arguments, "--model") != DefaultCursorModel {
		t.Fatalf("Cursor arguments = %v", arguments)
	}
}

type fakeCursorOptions struct {
	help        string
	loggedOut   bool
	authError   string
	output      string
	reviewError string
	delay       string
}

type fakeCursor struct {
	path        string
	arguments   string
	prompt      string
	environment string
	permissions string
}

func newFakeCursor(t *testing.T, options fakeCursorOptions) fakeCursor {
	t.Helper()
	root := t.TempDir()
	fake := fakeCursor{
		path: filepath.Join(root, "cursor-agent"), arguments: filepath.Join(root, "arguments.txt"),
		prompt: filepath.Join(root, "prompt.txt"), environment: filepath.Join(root, "environment.txt"),
		permissions: filepath.Join(root, "permissions.json"),
	}
	if options.help == "" {
		options.help = cursorHelp("text | json | stream-json", "plan, ask", "enabled, disabled")
	}
	if options.output == "" {
		options.output = cursorEnvelope(`{"findings":[],"overall_explanation":"No defects.","overall_confidence":0.95}`)
	}
	authOutput, err := json.Marshal(map[string]any{"status": "authenticated", "isAuthenticated": !options.loggedOut})
	if err != nil {
		t.Fatal(err)
	}
	authBlock := "printf '%s' " + shellQuote(string(authOutput)) + "\nexit 0"
	if options.authError != "" {
		authBlock = "printf '%s' " + shellQuote(options.authError) + " >&2\nexit 1"
	}
	reviewFailure := ""
	if options.reviewError != "" {
		reviewFailure = "printf '%b' " + shellQuote(options.reviewError) + " >&2\nexit 7\n"
	}
	delay := ""
	if options.delay != "" {
		delay = "sleep " + options.delay + "\n"
	}
	script := "#!/bin/sh\n" +
		"set -eu\n" +
		"if [ \"${1:-}\" = \"--version\" ]; then printf '%s\\n' '2026.07.23-e383d2b'; exit 0; fi\n" +
		"if [ \"${1:-}\" = \"--help\" ]; then printf '%s\\n' " + shellQuote(options.help) + "; exit 0; fi\n" +
		"if [ \"${1:-}\" = \"status\" ] && [ \"${2:-}\" = \"--help\" ]; then printf '%s\\n' '--format <format> choices: text, json'; exit 0; fi\n" +
		"if [ \"${1:-}\" = \"status\" ] && [ \"${2:-}\" = \"--format\" ]; then " + authBlock + "; fi\n" +
		"printf '%s\\n' \"$@\" > " + shellQuote(fake.arguments) + "\n" +
		"cat > " + shellQuote(fake.prompt) + "\n" +
		"env > " + shellQuote(fake.environment) + "\n" +
		"if [ -n \"${CURSOR_CONFIG_DIR:-}\" ] && [ -f \"$CURSOR_CONFIG_DIR/cli-config.json\" ]; then cat \"$CURSOR_CONFIG_DIR/cli-config.json\" > " + shellQuote(fake.permissions) + "; fi\n" +
		reviewFailure + delay +
		"printf '%s' " + shellQuote(options.output) + "\n"
	if err := os.WriteFile(fake.path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return fake
}

func cursorHelp(outputFormats, modes, sandboxModes string) string {
	return strings.Join([]string{
		"--print",
		"--output-format <format> " + outputFormats,
		"--model <model>",
		"--mode <mode> choices: " + modes,
		"--sandbox <mode> choices: " + sandboxModes,
		"--workspace <path>",
		"--trust",
		"status",
	}, "\n")
}

func cursorEnvelope(result string) string {
	encoded, err := json.Marshal(map[string]any{
		"type": "result", "subtype": "success", "is_error": false, "result": result,
		"session_id": "fake-session", "request_id": "fake-request",
	})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func cursorConfig(isolation protocol.Isolation, web bool, timeout time.Duration) config.Effective {
	return config.Effective{
		Engine:          config.Value[protocol.ProviderName]{Value: protocol.ProviderCursor, Source: config.SourceFlag},
		Model:           config.Value[string]{Value: "test-model", Source: config.SourceFlag},
		ReasoningEffort: config.Value[config.ReasoningEffort]{Value: config.ReasoningHigh, Source: config.SourceDefault},
		Timeout:         config.Value[config.Duration]{Value: config.Duration(timeout), Source: config.SourceFlag},
		Retries:         config.Value[int]{Value: 1, Source: config.SourceDefault},
		MaxBytes:        config.Value[int64]{Value: 1 << 20, Source: config.SourceDefault},
		Isolation:       config.Value[protocol.Isolation]{Value: isolation, Source: config.SourceFlag},
		WebAccess:       config.Value[bool]{Value: web, Source: config.SourceFlag},
	}
}
