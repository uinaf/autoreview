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

func TestCodexReviewStrictUsesFrozenStdinAndCanonicalResult(t *testing.T) {
	t.Parallel()

	repository := t.TempDir()
	fake := newFakeCodex(t, fakeCodexOptions{})
	effective := codexConfig(protocol.IsolationStrict, false, 5*time.Second)
	reviewer := NewCodex(CodexOptions{
		Repository: repository,
		Executable: fake.path,
		Environment: []string{
			"PATH=/usr/bin:/bin",
			"OPENAI_API_KEY=test-provider-secret",
			"ANTHROPIC_API_KEY=must-not-pass",
			"HOME=/private/home",
		},
	})
	result, err := reviewer.Review(context.Background(), Request{Prompt: "frozen review bundle", Config: effective})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider.Name != protocol.ProviderCodex || result.Provider.Model != "test-model" || result.Provider.Version != "0.146.0" {
		t.Fatalf("provider = %+v", result.Provider)
	}
	if result.Attempt.Outcome != protocol.AttemptValid || result.Attempt.Number != 1 || result.ProtocolRecovery.Applied {
		t.Fatalf("result metadata = %+v, %+v", result.Attempt, result.ProtocolRecovery)
	}
	if result.Isolation != protocol.IsolationStrict || result.WebAccess {
		t.Fatalf("result policy = isolation %q, web %t", result.Isolation, result.WebAccess)
	}
	if len(result.Review.Findings) != 0 || result.Review.OverallExplanation != "No defects." {
		t.Fatalf("review = %+v", result.Review)
	}
	if prompt := readTestFile(t, fake.prompt); prompt != "frozen review bundle" {
		t.Fatalf("provider stdin = %q", prompt)
	}
	arguments := strings.Split(strings.TrimSpace(readTestFile(t, fake.arguments)), "\n")
	for _, required := range []string{"--strict-config", "--sandbox", "read-only", "--ignore-user-config", "--ignore-rules", "--output-schema", "--output-last-message", `web_search="disabled"`, "features.multi_agent=false"} {
		if !contains(arguments, required) {
			t.Errorf("Codex arguments omitted %q: %v", required, arguments)
		}
	}
	if indexOf(arguments, "--sandbox") < indexOf(arguments, "exec") {
		t.Fatalf("strict sandbox flag was not scoped to exec: %v", arguments)
	}
	if contains(arguments, "--search") {
		t.Fatalf("Codex arguments enabled web search: %v", arguments)
	}
	environment := readTestFile(t, fake.environment)
	if !strings.Contains(environment, "OPENAI_API_KEY=test-provider-secret") || strings.Contains(environment, "ANTHROPIC_API_KEY") || strings.Contains(environment, "HOME=/private/home") {
		t.Fatalf("strict environment = %s", environment)
	}
	workspace := strings.TrimSpace(readTestFile(t, fake.directory))
	if _, err := os.Stat(workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("provider workspace was not cleaned: %v", err)
	}
}

func TestCodexReviewNativePreservesConfigurationAndEnablesWeb(t *testing.T) {
	t.Parallel()

	fake := newFakeCodex(t, fakeCodexOptions{})
	effective := codexConfig(protocol.IsolationNative, true, 5*time.Second)
	reviewer := NewCodex(CodexOptions{
		Repository: t.TempDir(),
		Executable: fake.path,
		Environment: []string{
			"PATH=/usr/bin:/bin",
			"HOME=/native/home",
			"CODEX_HOME=/native/codex",
		},
	})
	if _, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: effective}); err != nil {
		t.Fatal(err)
	}
	arguments := strings.Split(strings.TrimSpace(readTestFile(t, fake.arguments)), "\n")
	for _, forbidden := range []string{"--strict-config", "--sandbox", "--ignore-user-config", "--ignore-rules", `web_search="disabled"`} {
		if contains(arguments, forbidden) {
			t.Errorf("native arguments retained %q: %v", forbidden, arguments)
		}
	}
	if !contains(arguments, "--search") {
		t.Fatalf("native arguments omitted --search: %v", arguments)
	}
	environment := readTestFile(t, fake.environment)
	if !strings.Contains(environment, "HOME=/native/home") || !strings.Contains(environment, "CODEX_HOME=/native/codex") {
		t.Fatalf("native environment = %s", environment)
	}
}

func TestCodexReviewFailsCapabilityProbeBeforeInvocation(t *testing.T) {
	t.Parallel()

	fake := newFakeCodex(t, fakeCodexOptions{execHelp: "--ephemeral --skip-git-repo-check"})
	reviewer := NewCodex(CodexOptions{
		Repository:  t.TempDir(),
		Executable:  fake.path,
		Environment: []string{"PATH=/usr/bin:/bin", "OPENAI_API_KEY=test-provider-secret"},
	})
	_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: codexConfig(protocol.IsolationStrict, false, 5*time.Second)})
	_ = assertProviderError(t, err, protocol.FailureCapability)
	if _, err := os.Stat(fake.arguments); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("model was invoked after failed capability probe: %v", err)
	}
}

func TestCodexReviewReportsAuthenticationFailure(t *testing.T) {
	t.Parallel()

	fake := newFakeCodex(t, fakeCodexOptions{authError: "not logged in"})
	reviewer := NewCodex(CodexOptions{
		Repository:  t.TempDir(),
		Executable:  fake.path,
		Environment: []string{"PATH=/usr/bin:/bin", "HOME=/native/home"},
	})
	_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: codexConfig(protocol.IsolationNative, false, 5*time.Second)})
	_ = assertProviderError(t, err, protocol.FailureAuth)
}

func TestCodexReviewStrictExplainsCredentialRequirement(t *testing.T) {
	t.Parallel()

	fake := newFakeCodex(t, fakeCodexOptions{})
	reviewer := NewCodex(CodexOptions{Repository: t.TempDir(), Executable: fake.path, Environment: []string{"PATH=/usr/bin:/bin"}})
	_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: codexConfig(protocol.IsolationStrict, false, 5*time.Second)})
	failure := assertProviderError(t, err, protocol.FailureAuth)
	for _, expected := range []string{"strict isolation", "CODEX_API_KEY", "OPENAI_API_KEY", "--isolation native"} {
		if !strings.Contains(failure.Message, expected) {
			t.Fatalf("failure = %q", failure.Message)
		}
	}
	if _, err := os.Stat(fake.arguments); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("provider was probed without a strict credential: %v", err)
	}
}

func TestCodexReviewStrictReportsMissingExecutableBeforeCredential(t *testing.T) {
	t.Parallel()

	reviewer := NewCodex(CodexOptions{Repository: t.TempDir(), Executable: "missing-codex", Environment: []string{"PATH=/usr/bin:/bin"}})
	_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: codexConfig(protocol.IsolationStrict, false, 5*time.Second)})
	failure := assertProviderError(t, err, protocol.FailureCapability)
	if !strings.Contains(failure.Message, "was not found") || strings.Contains(failure.Message, "API_KEY") {
		t.Fatalf("failure = %q", failure.Message)
	}
}

func TestCodexReviewStrictExplainsInvalidCredentialRecovery(t *testing.T) {
	t.Parallel()

	fake := newFakeCodex(t, fakeCodexOptions{reviewError: "authentication failed"})
	reviewer := NewCodex(CodexOptions{
		Repository: t.TempDir(), Executable: fake.path,
		Environment: []string{"PATH=/usr/bin:/bin", "CODEX_API_KEY=invalid-secret"},
	})
	_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: codexConfig(protocol.IsolationStrict, false, 5*time.Second)})
	failure := assertProviderError(t, err, protocol.FailureAuth)
	if !strings.Contains(failure.Message, "verify CODEX_API_KEY or OPENAI_API_KEY") || !strings.Contains(failure.Message, "--isolation native") || strings.Contains(failure.Message, "invalid-secret") {
		t.Fatalf("failure = %q", failure.Message)
	}
}

func TestCodexReviewAcceptsExecCredentialWithoutLoginState(t *testing.T) {
	t.Parallel()

	fake := newFakeCodex(t, fakeCodexOptions{authError: "not logged in"})
	reviewer := NewCodex(CodexOptions{
		Repository:  t.TempDir(),
		Executable:  fake.path,
		Environment: []string{"PATH=/usr/bin:/bin", "CODEX_API_KEY=test-provider-secret"},
	})
	if _, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: codexConfig(protocol.IsolationStrict, false, 5*time.Second)}); err != nil {
		t.Fatal(err)
	}
	if environment := readTestFile(t, fake.environment); !strings.Contains(environment, "CODEX_API_KEY=test-provider-secret") {
		t.Fatalf("strict environment = %s", environment)
	}
}

func TestCodexReviewOmitsProviderFailureOutput(t *testing.T) {
	t.Parallel()

	fake := newFakeCodex(t, fakeCodexOptions{reviewError: "\033[31m" + providerOutputSentinel + " test-provider-secret\r"})
	reviewer := NewCodex(CodexOptions{
		Repository:  t.TempDir(),
		Executable:  fake.path,
		Environment: []string{"PATH=/usr/bin:/bin", "OPENAI_API_KEY=test-provider-secret"},
	})
	_, err := reviewer.Review(context.Background(), Request{Prompt: providerOutputSentinel, Config: codexConfig(protocol.IsolationStrict, false, 5*time.Second)})
	failure := assertProviderError(t, err, protocol.FailureProvider)
	if strings.Contains(failure.Message, providerOutputSentinel) || strings.Contains(failure.Message, "test-provider-secret") || strings.ContainsRune(failure.Message, '\x1b') {
		t.Fatalf("provider failure = %q", failure.Message)
	}
	if !strings.Contains(failure.Message, "exit code 7") || !strings.Contains(failure.Message, "private diagnostics") {
		t.Fatalf("provider failure lacks safe context: %q", failure.Message)
	}
	if failure.Attempt == nil || failure.Attempt.Outcome != protocol.AttemptFailed {
		t.Fatalf("attempt = %+v", failure.Attempt)
	}
}

func TestCodexReviewDistinguishesTimeoutAndCancellation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		timeout time.Duration
		cancel  bool
		class   protocol.FailureClass
	}{
		{name: "timeout", timeout: 80 * time.Millisecond, class: protocol.FailureTimeout},
		{name: "cancelled", timeout: 5 * time.Second, cancel: true, class: protocol.FailureCancelled},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeCodex(t, fakeCodexOptions{delay: "0.5"})
			reviewer := NewCodex(CodexOptions{
				Repository:  t.TempDir(),
				Executable:  fake.path,
				Environment: []string{"PATH=/usr/bin:/bin", "OPENAI_API_KEY=test-provider-secret"},
			})
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
			_, err := reviewer.Review(ctx, Request{Prompt: "bundle", Config: codexConfig(protocol.IsolationStrict, false, test.timeout)})
			_ = assertProviderError(t, err, test.class)
		})
	}
}

func TestCodexReviewUsesSingleTimeoutBudget(t *testing.T) {
	t.Parallel()

	fake := newFakeCodex(t, fakeCodexOptions{probeDelay: "0.06"})
	reviewer := NewCodex(CodexOptions{
		Repository:  t.TempDir(),
		Executable:  fake.path,
		Environment: []string{"PATH=/usr/bin:/bin", "CODEX_API_KEY=test-provider-secret"},
	})
	started := time.Now()
	_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: codexConfig(protocol.IsolationStrict, false, 150*time.Millisecond)})
	_ = assertProviderError(t, err, protocol.FailureTimeout)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Review() exceeded timeout budget: %s", elapsed)
	}
}

func TestCodexReviewRejectsMalformedOrInconsistentOutput(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		options fakeCodexOptions
	}{
		{name: "malformed review", options: fakeCodexOptions{result: `{"findings":[]}`}},
		{name: "envelope mismatch", options: fakeCodexOptions{envelopeMessage: `{"findings":[],"overall_explanation":"Different.","overall_confidence":0.9}`}},
		{name: "invalid envelope", options: fakeCodexOptions{rawEnvelope: "not-json\n"}},
		{name: "provider error sentinel", options: fakeCodexOptions{rawEnvelope: `{"type":"error","message":"` + providerOutputSentinel + `"}` + "\n"}},
		{
			name: "event after completion",
			options: fakeCodexOptions{rawEnvelope: strings.Join([]string{
				`{"type":"thread.started","thread_id":"fake-thread"}`,
				`{"type":"turn.started"}`,
				`{"type":"turn.completed"}`,
				`{"type":"item.completed","item":{"type":"agent_message","text":"late"}}`,
				"",
			}, "\n")},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeCodex(t, test.options)
			reviewer := NewCodex(CodexOptions{
				Repository:  t.TempDir(),
				Executable:  fake.path,
				Environment: []string{"PATH=/usr/bin:/bin", "OPENAI_API_KEY=test-provider-secret"},
			})
			_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: codexConfig(protocol.IsolationStrict, false, 5*time.Second)})
			failure := assertProviderError(t, err, protocol.FailureProtocol)
			if strings.Contains(failure.Message, providerOutputSentinel) {
				t.Fatalf("protocol failure disclosed provider output: %q", failure.Message)
			}
			if failure.Attempt == nil || failure.Attempt.Outcome != protocol.AttemptMalformed {
				t.Fatalf("attempt = %+v", failure.Attempt)
			}
		})
	}
}

func TestCodexReviewEnforcesOutputBounds(t *testing.T) {
	t.Parallel()

	valid := `{"findings":[],"overall_explanation":"No defects.","overall_confidence":0.95}`
	for _, test := range []struct {
		name    string
		options fakeCodexOptions
		class   protocol.FailureClass
		outcome protocol.AttemptOutcome
	}{
		{
			name:    "process stdout",
			options: fakeCodexOptions{rawEnvelope: strings.Repeat("x", int(providerStdoutLimit)+1)},
			class:   protocol.FailureProvider,
			outcome: protocol.AttemptFailed,
		},
		{
			name: "last message",
			options: fakeCodexOptions{
				result:          valid + strings.Repeat(" ", int(providerResultLimit)),
				envelopeMessage: valid + strings.Repeat(" ", int(providerResultLimit)),
			},
			class:   protocol.FailureProtocol,
			outcome: protocol.AttemptMalformed,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeCodex(t, test.options)
			reviewer := NewCodex(CodexOptions{
				Repository:  t.TempDir(),
				Executable:  fake.path,
				Environment: []string{"PATH=/usr/bin:/bin", "OPENAI_API_KEY=test-provider-secret"},
			})
			_, err := reviewer.Review(context.Background(), Request{Prompt: "bundle", Config: codexConfig(protocol.IsolationStrict, false, 5*time.Second)})
			failure := assertProviderError(t, err, test.class)
			if failure.Attempt == nil || failure.Attempt.Outcome != test.outcome {
				t.Fatalf("attempt = %+v", failure.Attempt)
			}
		})
	}
}

type fakeCodexOptions struct {
	topHelp         string
	execHelp        string
	result          string
	envelopeMessage string
	rawEnvelope     string
	authError       string
	reviewError     string
	delay           string
	probeDelay      string
}

type fakeCodex struct {
	path        string
	arguments   string
	prompt      string
	environment string
	directory   string
}

func newFakeCodex(t *testing.T, options fakeCodexOptions) fakeCodex {
	t.Helper()
	root := t.TempDir()
	fake := fakeCodex{
		path:        filepath.Join(root, "codex"),
		arguments:   filepath.Join(root, "arguments.txt"),
		prompt:      filepath.Join(root, "prompt.txt"),
		environment: filepath.Join(root, "environment.txt"),
		directory:   filepath.Join(root, "directory.txt"),
	}
	if options.topHelp == "" {
		options.topHelp = "--ask-for-approval --strict-config --search"
	}
	if options.execHelp == "" {
		options.execHelp = "--ephemeral --skip-git-repo-check --output-schema --output-last-message --json --cd --ignore-user-config --ignore-rules --sandbox"
	}
	if options.result == "" {
		options.result = `{"findings":[],"overall_explanation":"No defects.","overall_confidence":0.95}`
	}
	if options.envelopeMessage == "" {
		options.envelopeMessage = options.result
	}
	envelope := options.rawEnvelope
	if envelope == "" {
		message, err := json.Marshal(map[string]any{
			"type": "item.completed",
			"item": map[string]any{"id": "item_0", "type": "agent_message", "text": options.envelopeMessage},
		})
		if err != nil {
			t.Fatal(err)
		}
		envelope = strings.Join([]string{
			`{"type":"thread.started","thread_id":"fake-thread"}`,
			`{"type":"turn.started"}`,
			string(message),
			`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
			"",
		}, "\n")
	}
	resultPath := filepath.Join(root, "result.json")
	if err := os.WriteFile(resultPath, []byte(options.result), 0o600); err != nil {
		t.Fatal(err)
	}
	envelopePath := filepath.Join(root, "envelope.jsonl")
	if err := os.WriteFile(envelopePath, []byte(envelope), 0o600); err != nil {
		t.Fatal(err)
	}
	authBlock := "printf '%s\\n' 'Logged in using ChatGPT'\nexit 0"
	if options.authError != "" {
		authBlock = "printf '%s\\n' " + shellQuote(options.authError) + " >&2\nexit 1"
	}
	reviewFailure := ""
	if options.reviewError != "" {
		reviewFailure = "printf '%b' " + shellQuote(options.reviewError) + " >&2\nexit 7\n"
	}
	delay := ""
	if options.delay != "" {
		delay = "sleep " + options.delay + "\n"
	}
	probeDelay := ""
	if options.probeDelay != "" {
		probeDelay = "sleep " + options.probeDelay + "\n"
	}
	script := "#!/bin/sh\n" +
		"set -eu\n" +
		"fail_contract() { printf '%s\\n' 'unexpected Codex CLI arguments' >&2; exit 64; }\n" +
		"validate_review() {\n" +
		"  [ \"$#\" -ge 1 ] || return 1\n" +
		"  [ \"$1\" = '--ask-for-approval' ] || return 1; shift\n" +
		"  [ \"${1:-}\" = 'never' ] || return 1; shift\n" +
		"  [ \"${1:-}\" = '--model' ] || return 1; shift\n" +
		"  [ -n \"${1:-}\" ] || return 1; case \"$1\" in -*) return 1 ;; esac; shift\n" +
		"  [ \"${1:-}\" = '-c' ] || return 1; shift\n" +
		"  case \"${1:-}\" in 'model_reasoning_effort=\"low\"'|'model_reasoning_effort=\"medium\"'|'model_reasoning_effort=\"high\"'|'model_reasoning_effort=\"xhigh\"'|'model_reasoning_effort=\"max\"') ;; *) return 1 ;; esac; shift\n" +
		"  if [ \"${1:-}\" = '--search' ]; then shift; elif [ \"${1:-}\" = '-c' ] && [ \"${2:-}\" = 'web_search=\"disabled\"' ]; then shift 2; else return 1; fi\n" +
		"  strict=0\n" +
		"  if [ \"${1:-}\" = '--strict-config' ]; then\n" +
		"    strict=1; shift\n" +
		"    for setting in 'project_doc_max_bytes=0' 'features.shell_snapshot=false' 'features.hooks=false' 'features.plugins=false' 'features.multi_agent=false' 'skills.include_instructions=false' 'skills.config=[]' 'shell_environment_policy.inherit=\"core\"' 'shell_environment_policy.ignore_default_excludes=false' 'shell_environment_policy.set={GIT_CONFIG_GLOBAL=\"/dev/null\",GIT_CONFIG_SYSTEM=\"/dev/null\",GIT_TERMINAL_PROMPT=\"0\"}' 'shell_environment_policy.experimental_use_profile=false' 'allow_login_shell=false' 'default_permissions=\"autoreview\"' 'permissions.autoreview.filesystem={\":minimal\"=\"read\",\":workspace_roots\"=\"read\"}'; do\n" +
		"      [ \"${1:-}\" = '-c' ] || return 1; shift\n" +
		"      [ \"${1:-}\" = \"$setting\" ] || return 1; shift\n" +
		"    done\n" +
		"  fi\n" +
		"  [ \"${1:-}\" = 'exec' ] || return 1; shift\n" +
		"  [ \"${1:-}\" = '--json' ] || return 1; shift\n" +
		"  [ \"${1:-}\" = '--color' ] || return 1; shift\n" +
		"  [ \"${1:-}\" = 'never' ] || return 1; shift\n" +
		"  [ \"${1:-}\" = '--ephemeral' ] || return 1; shift\n" +
		"  [ \"${1:-}\" = '--skip-git-repo-check' ] || return 1; shift\n" +
		"  [ \"${1:-}\" = '--cd' ] || return 1; shift\n" +
		"  [ -d \"${1:-}\" ] || return 1; shift\n" +
		"  if [ \"$strict\" -eq 1 ]; then\n" +
		"    [ \"${1:-}\" = '--sandbox' ] || return 1; shift\n" +
		"    [ \"${1:-}\" = 'read-only' ] || return 1; shift\n" +
		"    [ \"${1:-}\" = '--ignore-user-config' ] || return 1; shift\n" +
		"    [ \"${1:-}\" = '--ignore-rules' ] || return 1; shift\n" +
		"  fi\n" +
		"  [ \"${1:-}\" = '--output-schema' ] || return 1; shift\n" +
		"  [ -f \"${1:-}\" ] || return 1; shift\n" +
		"  [ \"${1:-}\" = '--output-last-message' ] || return 1; shift\n" +
		"  [ -f \"${1:-}\" ] || return 1; shift\n" +
		"  [ \"$#\" -eq 1 ] && [ \"$1\" = '-' ]\n" +
		"}\n" +
		probeDelay +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = \"--version\" ]; then printf '%s\\n' 'codex-cli 0.146.0'; exit 0; fi\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = \"--help\" ]; then printf '%s\\n' " + shellQuote(options.topHelp) + "; exit 0; fi\n" +
		"if [ \"$#\" -eq 2 ] && [ \"$1\" = \"exec\" ] && [ \"$2\" = \"--help\" ]; then printf '%s\\n' " + shellQuote(options.execHelp) + "; exit 0; fi\n" +
		"if [ \"$#\" -eq 2 ] && [ \"$1\" = \"login\" ] && [ \"$2\" = \"status\" ]; then " + authBlock + "; fi\n" +
		"validate_review \"$@\" || fail_contract\n" +
		"printf '%s\\n' \"$@\" > " + shellQuote(fake.arguments) + "\n" +
		"cat > " + shellQuote(fake.prompt) + "\n" +
		"[ -s " + shellQuote(fake.prompt) + " ] || fail_contract\n" +
		"env > " + shellQuote(fake.environment) + "\n" +
		"pwd > " + shellQuote(fake.directory) + "\n" +
		reviewFailure +
		delay +
		"output=''\nprevious=''\nfor argument in \"$@\"; do\n  if [ \"$previous\" = \"--output-last-message\" ]; then output=\"$argument\"; fi\n  previous=\"$argument\"\ndone\n" +
		"test -n \"$output\"\n" +
		"cat " + shellQuote(resultPath) + " > \"$output\"\n" +
		"cat " + shellQuote(envelopePath) + "\n"
	writeTestExecutableAt(t, fake.path, script)
	return fake
}

func codexConfig(isolation protocol.Isolation, web bool, timeout time.Duration) config.Effective {
	return config.Effective{
		Engine:          config.Value[protocol.ProviderName]{Value: protocol.ProviderCodex, Source: config.SourceFlag},
		Model:           config.Value[string]{Value: "test-model", Source: config.SourceFlag},
		ReasoningEffort: config.Value[config.ReasoningEffort]{Value: config.ReasoningHigh, Source: config.SourceDefault},
		Timeout:         config.Value[config.Duration]{Value: config.Duration(timeout), Source: config.SourceFlag},
		Retries:         config.Value[int]{Value: 1, Source: config.SourceDefault},
		MaxBytes:        config.Value[int64]{Value: 1 << 20, Source: config.SourceDefault},
		Isolation:       config.Value[protocol.Isolation]{Value: isolation, Source: config.SourceFlag},
		WebAccess:       config.Value[bool]{Value: web, Source: config.SourceFlag},
	}
}

func assertProviderError(t *testing.T, err error, class protocol.FailureClass) *Error {
	t.Helper()
	var failure *Error
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want provider error", err)
	}
	if failure.Class != class {
		t.Fatalf("error class = %q, want %q: %v", failure.Class, class, failure)
	}
	return failure
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func contains(values []string, want string) bool {
	return indexOf(values, want) >= 0
}

func indexOf(values []string, want string) int {
	for index, value := range values {
		if value == want {
			return index
		}
	}
	return -1
}
