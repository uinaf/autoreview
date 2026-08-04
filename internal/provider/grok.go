package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/uinaf/autoreview/internal/config"
	"github.com/uinaf/autoreview/internal/protocol"
	contractschema "github.com/uinaf/autoreview/schema"
)

const (
	defaultGrokExecutable = "grok"
	grokMaxTurns          = "2"
)

var grokVersionPattern = regexp.MustCompile(`\b(\d+\.\d+\.\d+)\b`)

var (
	errGrokIncompleteTurn  = errors.New("Grok did not complete the bounded review turn")
	errGrokUnauthenticated = errors.New("Grok is not authenticated")
)

type GrokOptions struct {
	Repository  string
	Executable  string
	Environment []string
}

type Grok struct {
	repository  string
	executable  string
	environment []string
}

func NewGrok(options GrokOptions) *Grok {
	executable := options.Executable
	if executable == "" {
		executable = defaultGrokExecutable
	}
	environment := options.Environment
	if environment == nil {
		environment = os.Environ()
	}
	return &Grok{
		repository:  options.Repository,
		executable:  executable,
		environment: append([]string(nil), environment...),
	}
}

func (grok *Grok) Review(ctx context.Context, request Request) (result Result, returnError error) {
	started := time.Now()
	if err := request.Config.Validate(); err != nil {
		return Result{}, newFailure(protocol.FailureConfig, fmt.Sprintf("invalid provider config: %v", err), nil, nil)
	}
	if request.Config.Engine.Value != protocol.ProviderGrok {
		return Result{}, newFailure(protocol.FailureConfig, fmt.Sprintf("Grok adapter cannot run engine %q", request.Config.Engine.Value), nil, nil)
	}
	if !request.validPrompt() {
		return Result{}, newFailure(protocol.FailureConfig, "provider prompt must be non-empty valid UTF-8", nil, nil)
	}
	maximumPrompt := request.Config.MaxBytes.Value + providerPromptAllowance
	promptBytes, validLength := request.promptBytes()
	if !validLength || maximumPrompt < request.Config.MaxBytes.Value || promptBytes > maximumPrompt {
		return Result{}, newFailure(protocol.FailureConfig, fmt.Sprintf("provider prompt exceeds %d bytes", maximumPrompt), nil, nil)
	}
	reviewContext, cancelReview := context.WithTimeout(ctx, time.Duration(request.Config.Timeout.Value))
	defer cancelReview()
	repository, err := filepath.Abs(grok.repository)
	if err != nil {
		return Result{}, newFailure(protocol.FailureConfig, fmt.Sprintf("resolve reviewed repository: %v", err), nil, nil)
	}
	executable, err := discoverExecutable(grok.executable, repository, grok.environment)
	if err != nil {
		return Result{}, newFailure(protocol.FailureCapability, err.Error(), grok.environment, nil)
	}
	if failure := strictCredentialFailure(request.Config, protocol.ProviderGrok, grok.environment); failure != nil {
		return Result{}, failure
	}
	runtime, err := config.PrepareRuntime(request.Config, grok.environment)
	if err != nil {
		return Result{}, newFailure(protocol.FailureInternal, fmt.Sprintf("prepare provider runtime: %v", err), grok.environment, nil)
	}
	defer func() {
		if err := runtime.Close(); err != nil && returnError == nil {
			result = Result{}
			returnError = newFailure(protocol.FailureInternal, err.Error(), runtime.Environment(), nil)
		}
	}()
	version, err := grok.preflight(reviewContext, executable, runtime.Workspace, runtime.Environment(), request.Config)
	if err != nil {
		return Result{}, err
	}
	promptPath := filepath.Join(runtime.Workspace, "review.prompt")
	prompt, err := os.OpenFile(promptPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Result{}, newFailure(protocol.FailureInternal, fmt.Sprintf("create Grok prompt: %v", err), runtime.Environment(), nil)
	}
	if _, err := io.Copy(prompt, request.promptReader("")); err != nil {
		_ = prompt.Close()
		return Result{}, newFailure(protocol.FailureInternal, fmt.Sprintf("write Grok prompt: %v", err), runtime.Environment(), nil)
	}
	if err := prompt.Close(); err != nil {
		return Result{}, newFailure(protocol.FailureInternal, fmt.Sprintf("close Grok prompt: %v", err), runtime.Environment(), nil)
	}
	providerSchema, err := contractschema.GrokReviewV1()
	if err != nil {
		return Result{}, newFailure(protocol.FailureInternal, err.Error(), runtime.Environment(), nil)
	}
	model := request.Config.Model.Value
	if model == "" {
		model = DefaultGrokModel
	}
	process, processErr := runProcess(reviewContext, processSpec{
		Path:        executable,
		Arguments:   grokArguments(request.Config, runtime.Workspace, promptPath, string(providerSchema), model),
		Directory:   runtime.Workspace,
		Environment: runtime.Environment(),
		Timeout:     time.Duration(request.Config.Timeout.Value),
		StdoutLimit: providerStdoutLimit,
		StderrLimit: providerStderrLimit,
	})
	attempt := protocol.Attempt{Number: 1, DurationMS: process.Duration.Milliseconds()}
	if processErr != nil {
		class := classifyProcessFailure(processErr, process)
		attempt.Outcome = protocol.AttemptFailed
		attempt.ErrorClass = &class
		return Result{}, processFailure("Grok review", class, processErr, process, runtime.Environment(), &attempt, strictCredentialRecovery(request.Config, protocol.ProviderGrok))
	}
	review, err := decodeGrokEnvelope(process.Stdout)
	if err != nil {
		class := protocol.FailureProtocol
		attempt.Outcome = protocol.AttemptMalformed
		if errors.Is(err, errGrokIncompleteTurn) {
			class = protocol.FailureProvider
			attempt.Outcome = protocol.AttemptFailed
		}
		attempt.ErrorClass = &class
		if class == protocol.FailureProvider {
			return Result{}, newFailure(class, errGrokIncompleteTurn.Error(), runtime.Environment(), &attempt)
		}
		return Result{}, invalidProviderOutput("Grok", "result envelope", runtime.Environment(), &attempt)
	}
	attempt.Outcome = protocol.AttemptValid
	return Result{
		Review: review,
		Provider: protocol.Provider{
			Name:    protocol.ProviderGrok,
			Model:   model,
			Version: version,
		},
		Attempt:   attempt,
		Duration:  time.Since(started),
		Isolation: request.Config.Isolation.Value,
		WebAccess: request.Config.WebAccess.Value,
		ProtocolRecovery: protocol.ProtocolRecovery{
			Applied: false,
		},
	}, nil
}

func (grok *Grok) preflight(ctx context.Context, executable, workspace string, environment []string, effective config.Effective) (string, error) {
	timeout := 10 * time.Second
	if time.Duration(effective.Timeout.Value) < timeout {
		timeout = time.Duration(effective.Timeout.Value)
	}
	run := func(arguments ...string) (processResult, error) {
		return runProcess(ctx, processSpec{
			Path:        executable,
			Arguments:   arguments,
			Directory:   workspace,
			Environment: environment,
			Timeout:     timeout,
			StdoutLimit: probeOutputLimit,
			StderrLimit: probeOutputLimit,
		})
	}
	versionResult, err := run("--version")
	if err != nil {
		return "", probeFailure("Grok --version", err, versionResult, environment, protocol.FailureCapability)
	}
	match := grokVersionPattern.FindSubmatch(append(versionResult.Stdout, versionResult.Stderr...))
	if len(match) != 2 {
		return "", newFailure(protocol.FailureCapability, "Grok --version did not report a semantic version", environment, nil)
	}
	helpResult, err := run("--help")
	if err != nil {
		return "", probeFailure("Grok --help", err, helpResult, environment, protocol.FailureCapability)
	}
	required := []string{
		"--prompt-file", "--output-format", "--json-schema", "--model", "--reasoning-effort", "--max-turns",
		"--permission-mode", "--tools", "--disallowed-tools", "--allow", "--deny", "--no-plan", "--no-subagents", "--no-memory", "--disable-web-search",
		"--verbatim", "--cwd", "models",
	}
	if effective.Isolation.Value == protocol.IsolationStrict {
		required = append(required, "--sandbox")
	}
	help := string(helpResult.Stdout) + string(helpResult.Stderr)
	if missing := missingCapabilities(help, required); len(missing) != 0 {
		return "", newFailure(protocol.FailureCapability, "Grok is missing required flags: "+strings.Join(missing, ", "), environment, nil)
	}
	if !optionSupports(help, "--output-format", "json") || !optionSupports(help, "--permission-mode", "dontAsk") {
		return "", newFailure(protocol.FailureCapability, "Grok is missing required option values: --output-format=json or --permission-mode=dontAsk", environment, nil)
	}
	if environmentValue(environment, "XAI_API_KEY") == "" {
		authResult, err := run("models")
		if err != nil {
			return "", probeFailure("Grok authentication", err, authResult, environment, protocol.FailureAuth)
		}
		if err := decodeGrokAuth(append(authResult.Stdout, authResult.Stderr...)); err != nil {
			if errors.Is(err, errGrokUnauthenticated) {
				return "", newFailure(protocol.FailureAuth, "Grok is not authenticated; run grok login or set XAI_API_KEY, then retry", environment, nil)
			}
			return "", newFailure(protocol.FailureCapability, "Grok models returned an unsupported authentication status format", environment, nil)
		}
	}
	return string(match[1]), nil
}

func grokArguments(effective config.Effective, workspace, promptPath, schema, model string) []string {
	arguments := []string{
		"--prompt-file", promptPath,
		"--output-format", "json",
		"--json-schema", schema,
		"--model", model,
		"--reasoning-effort", string(effective.ReasoningEffort.Value),
		"--max-turns", grokMaxTurns,
		"--permission-mode", "dontAsk",
		"--no-plan",
		"--no-subagents",
		"--no-memory",
		"--verbatim",
		"--cwd", workspace,
		"--deny", "Bash",
		"--deny", "Edit",
		"--deny", "Write",
		"--deny", "Read",
		"--deny", "Grep",
		"--deny", "MCPTool",
	}
	if effective.WebAccess.Value {
		arguments = append(arguments,
			"--tools", "web_search,web_fetch",
			"--disallowed-tools", "Agent",
			"--allow", "WebFetch",
			"--allow", "WebSearch",
		)
	} else {
		arguments = append(arguments,
			"--tools", "web_search",
			"--disallowed-tools", "web_search,Agent",
			"--disable-web-search",
			"--deny", "WebFetch",
			"--deny", "WebSearch",
		)
	}
	if effective.Isolation.Value == protocol.IsolationStrict {
		arguments = append(arguments, "--sandbox", "workspace")
	}
	return arguments
}

func decodeGrokAuth(output []byte) error {
	text := string(bytes.TrimSpace(output))
	lower := strings.ToLower(text)
	if text == "" || containsAny(lower, "not authenticated", "not logged in", "logged out", "no active session") {
		return errGrokUnauthenticated
	}
	if !strings.Contains(lower, "you are logged in") || !strings.Contains(lower, "available models:") {
		return fmt.Errorf("unrecognized authentication status")
	}
	return nil
}

func decodeGrokEnvelope(output []byte) (protocol.Review, error) {
	output = bytes.TrimSpace(output)
	if len(output) == 0 {
		return protocol.Review{}, fmt.Errorf("Grok output is empty")
	}
	if err := protocol.RejectDuplicateKeys(output); err != nil {
		return protocol.Review{}, err
	}
	var envelope struct {
		Text                  string          `json:"text"`
		StopReason            string          `json:"stopReason"`
		SessionID             string          `json:"sessionId"`
		RequestID             string          `json:"requestId"`
		StructuredOutput      json.RawMessage `json:"structuredOutput"`
		StructuredOutputError string          `json:"structuredOutputError"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return protocol.Review{}, err
	}
	if envelope.StopReason != "end_turn" {
		return protocol.Review{}, fmt.Errorf("%w: stop reason %q", errGrokIncompleteTurn, envelope.StopReason)
	}
	if strings.TrimSpace(envelope.SessionID) == "" || strings.TrimSpace(envelope.RequestID) == "" {
		return protocol.Review{}, fmt.Errorf("%w: missing request identifiers", errGrokIncompleteTurn)
	}
	if strings.TrimSpace(envelope.StructuredOutputError) != "" {
		return protocol.Review{}, fmt.Errorf("Grok reported a structured output error")
	}
	structured := bytes.TrimSpace(envelope.StructuredOutput)
	if len(structured) == 0 || structured[0] != '{' {
		return protocol.Review{}, fmt.Errorf("Grok result is missing structuredOutput object")
	}
	structuredReview, err := protocol.DecodeReview(structured)
	if err != nil {
		return protocol.Review{}, err
	}
	textReview, err := protocol.DecodeReview([]byte(strings.TrimSpace(envelope.Text)))
	if err != nil {
		return protocol.Review{}, err
	}
	if !reflect.DeepEqual(structuredReview, textReview) {
		return protocol.Review{}, fmt.Errorf("Grok text and structuredOutput disagree")
	}
	return structuredReview, nil
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}
