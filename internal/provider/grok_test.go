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

	"github.com/uinaf/autoreview/internal/config"
	"github.com/uinaf/autoreview/internal/protocol"
)

func TestGrokReviewStrictUsesFrozenPromptAndBoundedPolicy(t *testing.T) {
	t.Parallel()

	fake := newFakeGrok(t, fakeGrokOptions{authError: "not logged in"})
	reviewer := NewGrok(GrokOptions{
		Repository: t.TempDir(), Executable: fake.path,
		Environment: []string{
			"PATH=/usr/bin:/bin",
			"XAI_API_KEY=test-provider-secret",
			"OPENAI_API_KEY=must-not-pass",
			"HOME=/private/home",
		},
	})
	result, err := reviewer.Review(context.Background(), Request{Prompt: "frozen review bundle", TrustedSuffix: "\nretry", Config: grokConfig(protocol.IsolationStrict, false, 5*time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider.Name != protocol.ProviderGrok || result.Provider.Model != "test-model" || result.Provider.Version != "0.2.118" {
		t.Fatalf("provider = %+v", result.Provider)
	}
	if result.Attempt.Outcome != protocol.AttemptValid || result.ProtocolRecovery.Applied || len(result.Review.Findings) != 0 {
		t.Fatalf("result = %+v", result)
	}
	if prompt := readTestFile(t, fake.prompt); prompt != "frozen review bundle\nretry" {
		t.Fatalf("provider prompt = %q", prompt)
	}
	arguments := strings.Split(strings.TrimSpace(readTestFile(t, fake.arguments)), "\n")
	promptPath := argumentAfter(arguments, "--prompt-file")
	if _, err := os.Stat(promptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary Grok prompt survived review: %v", err)
	}
	for _, required := range []string{
		"--prompt-file", "--output-format", "json", "--json-schema", "--model", "test-model", "--reasoning-effort", "high",
		"--max-turns", "2", "--permission-mode", "dontAsk", "--no-plan", "--no-subagents", "--no-memory", "--verbatim",
		"--cwd", "--deny", "Bash", "Edit", "Write", "Read", "Grep", "MCPTool", "--tools", "--disallowed-tools",
		"web_search", "web_search,search_tool,use_tool,Agent", "--disable-web-search", "WebFetch", "WebSearch",
		"--sandbox", "workspace",
	} {
		if !contains(arguments, required) {
			t.Errorf("Grok arguments omitted %q: %v", required, arguments)
		}
	}
	if argumentAfter(arguments, "--tools") != "web_search" || argumentAfter(arguments, "--disallowed-tools") != "web_search,search_tool,use_tool,Agent" {
		t.Fatalf("Grok strict tool inventory = %v", arguments)
	}
	for _, tool := range []string{"WebFetch", "WebSearch"} {
		if !containsArgumentPair(arguments, "--deny", tool) || containsArgumentPair(arguments, "--allow", tool) {
			t.Fatalf("Grok strict web policy for %s = %v", tool, arguments)
		}
	}
	schema := argumentAfter(arguments, "--json-schema")
	if strings.Contains(schema, `"not"`) || !strings.Contains(schema, `"findings"`) {
		t.Fatalf("Grok schema projection = %s", schema)
	}
	environment := readTestFile(t, fake.environment)
	if !strings.Contains(environment, "XAI_API_KEY=test-provider-secret") || !strings.Contains(environment, "GROK_HOME=") || strings.Contains(environment, "OPENAI_API_KEY") || strings.Contains(environment, "HOME=/private/home") {
		t.Fatalf("strict environment = %s", environment)
	}
}

func TestGrokReviewNativeUsesSessionAuthAndExplicitWebPolicy(t *testing.T) {
	t.Parallel()

	fake := newFakeGrok(t, fakeGrokOptions{})
	reviewer := NewGrok(GrokOptions{
		Repository: t.TempDir(), Executable: fake.path,
		Environment: []string{"PATH=/usr/bin:/bin", "HOME=/native/home", "GROK_HOME=/native/grok"},
	})
	result, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: grokConfig(protocol.IsolationNative, true, 5*time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if !result.WebAccess || result.Isolation != protocol.IsolationNative {
		t.Fatalf("result policy = %+v", result)
	}
	arguments := strings.Split(strings.TrimSpace(readTestFile(t, fake.arguments)), "\n")
	for _, required := range []string{"--allow", "WebFetch", "WebSearch"} {
		if !contains(arguments, required) {
			t.Errorf("Grok web arguments omitted %q: %v", required, arguments)
		}
	}
	if argumentAfter(arguments, "--tools") != "web_search,web_fetch" || argumentAfter(arguments, "--disallowed-tools") != "search_tool,use_tool,Agent" {
		t.Fatalf("Grok web tool inventory = %v", arguments)
	}
	for _, forbidden := range []string{"--disable-web-search", "--sandbox"} {
		if contains(arguments, forbidden) {
			t.Errorf("Grok native web arguments retained %q: %v", forbidden, arguments)
		}
	}
	environment := readTestFile(t, fake.environment)
	if !strings.Contains(environment, "HOME=/native/home") || !strings.Contains(environment, "GROK_HOME=/native/grok") {
		t.Fatalf("native environment = %s", environment)
	}
}

func TestGrokReviewFailsCapabilityProbeBeforeInvocation(t *testing.T) {
	t.Parallel()

	fake := newFakeGrok(t, fakeGrokOptions{help: "--prompt-file --output-format"})
	reviewer := NewGrok(GrokOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "XAI_API_KEY=secret"}})
	_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: grokConfig(protocol.IsolationStrict, false, 5*time.Second)})
	_ = assertProviderError(t, err, protocol.FailureCapability)
	if _, err := os.Stat(fake.arguments); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("model was invoked after failed capability probe: %v", err)
	}
}

func TestGrokReviewReportsAuthenticationFailure(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		options fakeGrokOptions
	}{
		{name: "not authenticated", options: fakeGrokOptions{loggedOut: true}},
		{name: "not logged in", options: fakeGrokOptions{authStatus: "You are not logged in.\nAvailable models:"}},
		{name: "logged out", options: fakeGrokOptions{authStatus: "You are logged out.\nAvailable models:"}},
		{name: "no active session", options: fakeGrokOptions{authStatus: "No active session.\nAvailable models:"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeGrok(t, test.options)
			reviewer := NewGrok(GrokOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "HOME=/native/home"}})
			_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: grokConfig(protocol.IsolationNative, false, 5*time.Second)})
			_ = assertProviderError(t, err, protocol.FailureAuth)
		})
	}
}

func TestGrokReviewReportsAuthenticationFormatDriftAsCapabilityFailure(t *testing.T) {
	t.Parallel()

	fake := newFakeGrok(t, fakeGrokOptions{authStatus: "Account ready."})
	reviewer := NewGrok(GrokOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "HOME=/native/home"}})
	_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: grokConfig(protocol.IsolationNative, false, 5*time.Second)})
	_ = assertProviderError(t, err, protocol.FailureCapability)
}

func TestGrokReviewStrictExplainsCredentialRequirement(t *testing.T) {
	t.Parallel()

	fake := newFakeGrok(t, fakeGrokOptions{})
	reviewer := NewGrok(GrokOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin"}})
	_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: grokConfig(protocol.IsolationStrict, false, 5*time.Second)})
	failure := assertProviderError(t, err, protocol.FailureAuth)
	if !strings.Contains(failure.Message, "strict isolation requires XAI_API_KEY") || !strings.Contains(failure.Message, "--isolation native") {
		t.Fatalf("failure = %q", failure.Message)
	}
}

func TestGrokReviewStrictReportsMissingExecutableBeforeCredential(t *testing.T) {
	t.Parallel()

	reviewer := NewGrok(GrokOptions{Repository: t.TempDir(), Executable: "missing-grok", Environment: []string{"PATH=/usr/bin:/bin"}})
	_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: grokConfig(protocol.IsolationStrict, false, 5*time.Second)})
	failure := assertProviderError(t, err, protocol.FailureCapability)
	if !strings.Contains(failure.Message, "was not found") || strings.Contains(failure.Message, "API_KEY") {
		t.Fatalf("failure = %q", failure.Message)
	}
}

func TestGrokReviewRejectsMalformedEnvelopeAndReview(t *testing.T) {
	t.Parallel()

	validReview := `{"findings":[],"overall_explanation":"No defects.","overall_confidence":0.95}`
	for _, test := range []struct {
		name             string
		output           string
		incomplete       bool
		wantMessage      string
		forbiddenMessage string
	}{
		{name: "not JSON", output: "not-json"},
		{name: "cancelled zero exit", output: `{"text":"","stopReason":"cancelled","sessionId":"s","requestId":"r","structuredOutput":null,"structuredOutputError":"model did not produce structured output"}`, incomplete: true, wantMessage: "stop reason cancelled"},
		{name: "unknown stop reason", output: `{"text":"","stopReason":"private-provider-detail","sessionId":"s","requestId":"r","structuredOutput":null}`, incomplete: true, wantMessage: "stop reason was not end_turn", forbiddenMessage: "private-provider-detail"},
		{name: "missing request identifier", output: `{"text":"` + escapeJSONString(validReview) + `","stopReason":"end_turn","sessionId":"","requestId":"r","structuredOutput":` + validReview + `}`, incomplete: true, wantMessage: "missing request identifiers"},
		{name: "missing structured output", output: `{"text":"{\"findings\":[]}","stopReason":"end_turn","sessionId":"s","requestId":"r","structuredOutput":null}`},
		{name: "duplicate envelope key", output: `{"text":"x","text":"x","stopReason":"end_turn","sessionId":"s","requestId":"r","structuredOutput":{}}`},
		{name: "invalid canonical review", output: grokEnvelope(`{"findings":[]}`)},
		{name: "duplicate review key", output: `{"text":"` + escapeJSONString(`{"findings":[],"overall_explanation":"No defects.","overall_confidence":0.95}`) + `","stopReason":"end_turn","sessionId":"s","requestId":"r","structuredOutput":{"findings":[],"findings":[],"overall_explanation":"No defects.","overall_confidence":0.95}}`},
		{name: "text mismatch", output: grokMismatchedEnvelope(validReview, `{"findings":[],"overall_explanation":"Different.","overall_confidence":0.95}`)},
		{name: "valid control", output: grokEnvelope(validReview)},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeGrok(t, fakeGrokOptions{output: test.output})
			reviewer := NewGrok(GrokOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "XAI_API_KEY=secret"}})
			_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: grokConfig(protocol.IsolationStrict, false, 5*time.Second)})
			if test.name == "valid control" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			expectedClass := protocol.FailureProtocol
			expectedOutcome := protocol.AttemptMalformed
			if test.incomplete {
				expectedClass = protocol.FailureProvider
				expectedOutcome = protocol.AttemptFailed
			}
			failure := assertProviderError(t, err, expectedClass)
			if failure.Attempt == nil || failure.Attempt.Outcome != expectedOutcome {
				t.Fatalf("attempt = %+v", failure.Attempt)
			}
			if test.wantMessage != "" && !strings.Contains(failure.Message, test.wantMessage) {
				t.Fatalf("failure message = %q, want %q", failure.Message, test.wantMessage)
			}
			if test.forbiddenMessage != "" && strings.Contains(failure.Message, test.forbiddenMessage) {
				t.Fatalf("failure message exposed private provider detail: %q", failure.Message)
			}
		})
	}
}

func TestGrokReviewDistinguishesTimeoutAndCancellation(t *testing.T) {
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
			fake := newFakeGrok(t, fakeGrokOptions{delay: "0.5"})
			reviewer := NewGrok(GrokOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "XAI_API_KEY=secret"}})
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
			_, err := reviewer.Review(ctx, Request{Prompt: "bundle", Config: grokConfig(protocol.IsolationStrict, false, test.timeout)})
			_ = assertProviderError(t, err, test.class)
		})
	}
}

func TestGrokReviewEnforcesOutputBounds(t *testing.T) {
	t.Parallel()

	fake := newFakeGrok(t, fakeGrokOptions{output: strings.Repeat("x", int(providerStdoutLimit)+1)})
	reviewer := NewGrok(GrokOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "XAI_API_KEY=secret"}})
	_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: grokConfig(protocol.IsolationStrict, false, 5*time.Second)})
	failure := assertProviderError(t, err, protocol.FailureProvider)
	if failure.Attempt == nil || failure.Attempt.Outcome != protocol.AttemptFailed {
		t.Fatalf("attempt = %+v", failure.Attempt)
	}
}

func TestGrokReviewUsesExplicitDefaultModel(t *testing.T) {
	t.Parallel()

	effective := grokConfig(protocol.IsolationStrict, false, 5*time.Second)
	effective.Model = config.Value[string]{Source: config.SourceDefault}
	fake := newFakeGrok(t, fakeGrokOptions{})
	reviewer := NewGrok(GrokOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "XAI_API_KEY=secret"}})
	result, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: effective})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider.Model != DefaultGrokModel {
		t.Fatalf("provider model = %q", result.Provider.Model)
	}
	arguments := strings.Split(strings.TrimSpace(readTestFile(t, fake.arguments)), "\n")
	if argumentAfter(arguments, "--model") != DefaultGrokModel {
		t.Fatalf("Grok arguments = %v", arguments)
	}
}

func TestGrokReviewOmitsProviderFailureOutput(t *testing.T) {
	t.Parallel()

	fake := newFakeGrok(t, fakeGrokOptions{reviewError: "\033[31m" + providerOutputSentinel + " test-provider-secret\r"})
	reviewer := NewGrok(GrokOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin", "XAI_API_KEY=test-provider-secret"}})
	_, err := reviewer.Review(context.Background(), Request{Prompt: providerOutputSentinel, Config: grokConfig(protocol.IsolationStrict, false, 5*time.Second)})
	failure := assertProviderError(t, err, protocol.FailureProvider)
	if strings.Contains(failure.Message, providerOutputSentinel) || strings.Contains(failure.Message, "test-provider-secret") || strings.ContainsRune(failure.Message, '\x1b') {
		t.Fatalf("provider failure = %q", failure.Message)
	}
	if !strings.Contains(failure.Message, "exit code 7") || !strings.Contains(failure.Message, "private diagnostics") {
		t.Fatalf("provider failure lacks safe context: %q", failure.Message)
	}
}

type fakeGrokOptions struct {
	help        string
	loggedOut   bool
	authError   string
	authStatus  string
	output      string
	reviewError string
	delay       string
}

type fakeGrok struct {
	path        string
	arguments   string
	prompt      string
	environment string
}

func newFakeGrok(t *testing.T, options fakeGrokOptions) fakeGrok {
	t.Helper()
	root := t.TempDir()
	fake := fakeGrok{
		path: filepath.Join(root, "grok"), arguments: filepath.Join(root, "arguments.txt"),
		prompt: filepath.Join(root, "prompt.txt"), environment: filepath.Join(root, "environment.txt"),
	}
	if options.help == "" {
		options.help = grokHelp()
	}
	if options.output == "" {
		options.output = grokEnvelope(`{"findings":[],"overall_explanation":"No defects.","overall_confidence":0.95}`)
	}
	outputPath := filepath.Join(root, "output.json")
	if err := os.WriteFile(outputPath, []byte(options.output), 0o600); err != nil {
		t.Fatal(err)
	}
	authBlock := "printf '%s\\n' 'You are logged in with grok.com.\n\nDefault model: grok-4.5\n\nAvailable models:\n  * grok-4.5 (default)'\nexit 0"
	if options.loggedOut {
		authBlock = "printf '%s\\n' 'You are not authenticated.\n\nDefault model: grok-4.5\n\nAvailable models:\n  * grok-4.5 (default)'\nexit 0"
	}
	if options.authStatus != "" {
		authBlock = "printf '%s\\n' " + shellQuote(options.authStatus) + "\nexit 0"
	}
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
		"fail_contract() { printf '%s\\n' 'unexpected Grok CLI arguments' >&2; exit 64; }\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = '--version' ]; then printf '%s\\n' 'grok 0.2.118 (fake)'; exit 0; fi\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = '--help' ]; then printf '%s\\n' " + shellQuote(options.help) + "; exit 0; fi\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = 'models' ]; then " + authBlock + "; fi\n" +
		"printf '%s\\n' \"$@\" > " + shellQuote(fake.arguments) + "\n" +
		"[ \"${1:-}\" = '--prompt-file' ] || fail_contract; shift\n" +
		"prompt_path=${1:-}; [ -f \"$prompt_path\" ] || fail_contract; shift\n" +
		"[ \"${1:-}\" = '--output-format' ] || fail_contract; shift; [ \"${1:-}\" = 'json' ] || fail_contract; shift\n" +
		"[ \"${1:-}\" = '--json-schema' ] || fail_contract; shift; case \"${1:-}\" in '{'*'}') ;; *) fail_contract ;; esac; shift\n" +
		"[ \"${1:-}\" = '--model' ] || fail_contract; shift; [ -n \"${1:-}\" ] || fail_contract; case \"$1\" in -*) fail_contract ;; esac; shift\n" +
		"[ \"${1:-}\" = '--reasoning-effort' ] || fail_contract; shift; [ -n \"${1:-}\" ] || fail_contract; shift\n" +
		"[ \"${1:-}\" = '--max-turns' ] || fail_contract; shift; [ \"${1:-}\" = '2' ] || fail_contract; shift\n" +
		"[ \"${1:-}\" = '--permission-mode' ] || fail_contract; shift; [ \"${1:-}\" = 'dontAsk' ] || fail_contract; shift\n" +
		"for flag in --no-plan --no-subagents --no-memory --verbatim; do [ \"${1:-}\" = \"$flag\" ] || fail_contract; shift; done\n" +
		"[ \"${1:-}\" = '--cwd' ] || fail_contract; shift; [ -d \"${1:-}\" ] || fail_contract; shift\n" +
		"for tool in Bash Edit Write Read Grep MCPTool; do [ \"${1:-}\" = '--deny' ] || fail_contract; shift; [ \"${1:-}\" = \"$tool\" ] || fail_contract; shift; done\n" +
		"[ \"${1:-}\" = '--tools' ] || fail_contract; shift; if [ \"${1:-}\" = 'web_search,web_fetch' ]; then shift; [ \"${1:-}\" = '--disallowed-tools' ] || fail_contract; shift; [ \"${1:-}\" = 'search_tool,use_tool,Agent' ] || fail_contract; shift; [ \"${1:-}\" = '--allow' ] || fail_contract; shift; [ \"${1:-}\" = 'WebFetch' ] || fail_contract; shift; [ \"${1:-}\" = '--allow' ] || fail_contract; shift; [ \"${1:-}\" = 'WebSearch' ] || fail_contract; shift; else [ \"${1:-}\" = 'web_search' ] || fail_contract; shift; [ \"${1:-}\" = '--disallowed-tools' ] || fail_contract; shift; [ \"${1:-}\" = 'web_search,search_tool,use_tool,Agent' ] || fail_contract; shift; [ \"${1:-}\" = '--disable-web-search' ] || fail_contract; shift; for tool in WebFetch WebSearch; do [ \"${1:-}\" = '--deny' ] || fail_contract; shift; [ \"${1:-}\" = \"$tool\" ] || fail_contract; shift; done; fi\n" +
		"if [ \"${1:-}\" = '--sandbox' ]; then shift; [ \"${1:-}\" = 'workspace' ] || fail_contract; shift; fi\n" +
		"[ \"$#\" -eq 0 ] || fail_contract\n" +
		"cat \"$prompt_path\" > " + shellQuote(fake.prompt) + "\n" +
		"env > " + shellQuote(fake.environment) + "\n" +
		reviewFailure + delay +
		"cat " + shellQuote(outputPath) + "\n"
	writeTestExecutableAt(t, fake.path, script)
	return fake
}

func grokHelp() string {
	return strings.Join([]string{
		"--prompt-file <path>",
		"--output-format <format> possible values: plain, json, streaming-json",
		"--json-schema <schema>",
		"--model <model>",
		"--reasoning-effort <effort>",
		"--max-turns <n>",
		"--permission-mode <mode> possible values: default, dontAsk",
		"--tools <tools>",
		"--disallowed-tools <tools>",
		"--allow <rule>",
		"--deny <rule>",
		"--no-plan", "--no-subagents", "--no-memory", "--disable-web-search", "--verbatim", "--cwd <path>", "--sandbox <profile>", "models",
	}, "\n")
}

func grokEnvelope(review string) string {
	return grokMismatchedEnvelope(review, review)
}

func grokMismatchedEnvelope(textReview, structuredReview string) string {
	var structured any
	if err := json.Unmarshal([]byte(structuredReview), &structured); err != nil {
		panic(err)
	}
	encoded, err := json.Marshal(map[string]any{
		"text": textReview, "stopReason": "end_turn", "sessionId": "fake-session", "requestId": "fake-request", "structuredOutput": structured,
	})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func escapeJSONString(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded[1 : len(encoded)-1])
}

func containsArgumentPair(arguments []string, flag, value string) bool {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == flag && arguments[index+1] == value {
			return true
		}
	}
	return false
}

func grokConfig(isolation protocol.Isolation, web bool, timeout time.Duration) config.Effective {
	return config.Effective{
		Engine:          config.Value[protocol.ProviderName]{Value: protocol.ProviderGrok, Source: config.SourceFlag},
		Model:           config.Value[string]{Value: "test-model", Source: config.SourceFlag},
		ReasoningEffort: config.Value[config.ReasoningEffort]{Value: config.ReasoningHigh, Source: config.SourceDefault},
		Timeout:         config.Value[config.Duration]{Value: config.Duration(timeout), Source: config.SourceFlag},
		Retries:         config.Value[int]{Value: 1, Source: config.SourceDefault},
		MaxBytes:        config.Value[int64]{Value: 1 << 20, Source: config.SourceDefault},
		Isolation:       config.Value[protocol.Isolation]{Value: isolation, Source: config.SourceFlag},
		WebAccess:       config.Value[bool]{Value: web, Source: config.SourceFlag},
	}
}
