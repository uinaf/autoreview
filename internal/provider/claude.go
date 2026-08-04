package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/uinaf/autoreview/internal/config"
	"github.com/uinaf/autoreview/internal/protocol"
	contractschema "github.com/uinaf/autoreview/schema"
)

const defaultClaudeExecutable = "claude"

var claudeVersionPattern = regexp.MustCompile(`\b(\d+\.\d+\.\d+)\b`)

type ClaudeOptions struct {
	Repository  string
	Executable  string
	Environment []string
}

type Claude struct {
	repository  string
	executable  string
	environment []string
}

func NewClaude(options ClaudeOptions) *Claude {
	executable := options.Executable
	if executable == "" {
		executable = defaultClaudeExecutable
	}
	environment := options.Environment
	if environment == nil {
		environment = os.Environ()
	}
	return &Claude{
		repository:  options.Repository,
		executable:  executable,
		environment: append([]string(nil), environment...),
	}
}

func (claude *Claude) Review(ctx context.Context, request Request) (result Result, returnError error) {
	started := time.Now()
	if err := request.Config.Validate(); err != nil {
		return Result{}, newFailure(protocol.FailureConfig, fmt.Sprintf("invalid provider config: %v", err), nil, nil)
	}
	if request.Config.Engine.Value != protocol.ProviderClaude {
		return Result{}, newFailure(protocol.FailureConfig, fmt.Sprintf("Claude adapter cannot run engine %q", request.Config.Engine.Value), nil, nil)
	}
	switch request.Config.ReasoningEffort.Value {
	case config.ReasoningLow, config.ReasoningMedium, config.ReasoningHigh, config.ReasoningXHigh, config.ReasoningMax:
	default:
		return Result{}, newFailure(protocol.FailureConfig, fmt.Sprintf("Claude does not support reasoning effort %q", request.Config.ReasoningEffort.Value), nil, nil)
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
	repository, err := filepath.Abs(claude.repository)
	if err != nil {
		return Result{}, newFailure(protocol.FailureConfig, fmt.Sprintf("resolve reviewed repository: %v", err), nil, nil)
	}
	executable, err := discoverExecutable(claude.executable, repository, claude.environment)
	if err != nil {
		return Result{}, newFailure(protocol.FailureCapability, err.Error(), claude.environment, nil)
	}
	if failure := strictCredentialFailure(request.Config, protocol.ProviderClaude, claude.environment); failure != nil {
		return Result{}, failure
	}
	runtime, err := config.PrepareRuntime(request.Config, claude.environment)
	if err != nil {
		return Result{}, newFailure(protocol.FailureInternal, fmt.Sprintf("prepare provider runtime: %v", err), claude.environment, nil)
	}
	defer func() {
		if err := runtime.Close(); err != nil && returnError == nil {
			result = Result{}
			returnError = newFailure(protocol.FailureInternal, err.Error(), runtime.Environment(), nil)
		}
	}()
	environment := runtime.Environment()
	if request.Config.Isolation.Value == protocol.IsolationStrict {
		environment = setEnvironmentValue(environment, "CLAUDE_CODE_DISABLE_AUTO_MEMORY", "1")
	}
	version, err := claude.preflight(reviewContext, executable, runtime.Workspace, environment, request.Config)
	if err != nil {
		return Result{}, err
	}
	providerSchema, err := contractschema.ClaudeReviewV1()
	if err != nil {
		return Result{}, newFailure(protocol.FailureInternal, err.Error(), environment, nil)
	}
	model := request.Config.Model.Value
	if model == "" {
		model = DefaultClaudeModel
	}
	process, processErr := runProcess(reviewContext, processSpec{
		Path:        executable,
		Arguments:   claudeArguments(request.Config, string(providerSchema), model),
		Directory:   runtime.Workspace,
		Environment: environment,
		Input:       request.promptReader(""),
		Timeout:     time.Duration(request.Config.Timeout.Value),
		StdoutLimit: providerStdoutLimit,
		StderrLimit: providerStderrLimit,
	})
	attempt := protocol.Attempt{Number: 1, DurationMS: process.Duration.Milliseconds()}
	if processErr != nil {
		class := classifyProcessFailure(processErr, process)
		attempt.Outcome = protocol.AttemptFailed
		attempt.ErrorClass = &class
		return Result{}, processFailure("Claude review", class, processErr, process, environment, &attempt, strictCredentialRecovery(request.Config, protocol.ProviderClaude))
	}
	reviewData, err := decodeClaudeEnvelope(process.Stdout)
	if err != nil {
		class := protocol.FailureProtocol
		attempt.Outcome = protocol.AttemptMalformed
		attempt.ErrorClass = &class
		return Result{}, invalidProviderOutput("Claude", "result envelope", environment, &attempt)
	}
	review, err := protocol.DecodeReview(reviewData)
	if err != nil {
		class := protocol.FailureProtocol
		attempt.Outcome = protocol.AttemptMalformed
		attempt.ErrorClass = &class
		return Result{}, invalidProviderOutput("Claude", "review document", environment, &attempt)
	}
	attempt.Outcome = protocol.AttemptValid
	return Result{
		Review: review,
		Provider: protocol.Provider{
			Name:    protocol.ProviderClaude,
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

func (claude *Claude) preflight(ctx context.Context, executable, workspace string, environment []string, effective config.Effective) (string, error) {
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
		return "", probeFailure("Claude --version", err, versionResult, environment, protocol.FailureCapability)
	}
	match := claudeVersionPattern.FindSubmatch(append(versionResult.Stdout, versionResult.Stderr...))
	if len(match) != 2 {
		return "", newFailure(protocol.FailureCapability, "Claude --version did not report a semantic version", environment, nil)
	}
	helpResult, err := run("--help")
	if err != nil {
		return "", probeFailure("Claude --help", err, helpResult, environment, protocol.FailureCapability)
	}
	required := []string{"--print", "--no-session-persistence", "--output-format", "--json-schema", "--model", "--effort", "--tools", "--permission-mode", "--no-chrome"}
	if effective.Isolation.Value == protocol.IsolationStrict {
		required = append(required, "--safe-mode", "--setting-sources", "--strict-mcp-config", "--disallowedTools")
	}
	if effective.WebAccess.Value {
		required = append(required, "--allowedTools")
	}
	if missing := missingCapabilities(string(helpResult.Stdout)+string(helpResult.Stderr), required); len(missing) != 0 {
		return "", newFailure(protocol.FailureCapability, "Claude is missing required flags: "+strings.Join(missing, ", "), environment, nil)
	}
	if environmentValue(environment, "ANTHROPIC_API_KEY") == "" {
		authResult, err := run("auth", "status", "--json")
		if err != nil {
			return "", probeFailure("Claude authentication", err, authResult, environment, protocol.FailureAuth)
		}
		if err := decodeClaudeAuth(authResult.Stdout); err != nil {
			return "", newFailure(protocol.FailureAuth, "Claude returned an invalid authentication status; authenticate the provider CLI and retry", environment, nil)
		}
	}
	return string(match[1]), nil
}

func claudeArguments(effective config.Effective, schema, model string) []string {
	arguments := make([]string, 0, 32)
	if effective.Isolation.Value == protocol.IsolationStrict {
		arguments = append(arguments,
			"--safe-mode",
			"--setting-sources", "user",
			"--strict-mcp-config",
			"--disallowedTools", "mcp__*",
		)
	}
	arguments = append(arguments,
		"--print",
		"--no-session-persistence",
		"--output-format", "json",
		"--json-schema", schema,
		"--permission-mode", "dontAsk",
		"--no-chrome",
	)
	if effective.WebAccess.Value {
		arguments = append(arguments, "--tools", "WebSearch", "--allowedTools", "WebSearch")
	} else {
		arguments = append(arguments, "--tools", "")
	}
	arguments = append(arguments,
		"--model", model,
		"--effort", string(effective.ReasoningEffort.Value),
	)
	return arguments
}

func decodeClaudeAuth(output []byte) error {
	output = bytes.TrimSpace(output)
	if len(output) == 0 {
		return fmt.Errorf("status output is empty")
	}
	if err := protocol.RejectDuplicateKeys(output); err != nil {
		return err
	}
	var status struct {
		LoggedIn   *bool  `json:"loggedIn"`
		AuthMethod string `json:"authMethod"`
		Provider   string `json:"apiProvider"`
	}
	if err := json.Unmarshal(output, &status); err != nil {
		return err
	}
	if status.LoggedIn == nil || !*status.LoggedIn || strings.TrimSpace(status.AuthMethod) == "" || strings.TrimSpace(status.Provider) == "" {
		return fmt.Errorf("not logged in")
	}
	return nil
}

func decodeClaudeEnvelope(output []byte) ([]byte, error) {
	output = bytes.TrimSpace(output)
	if len(output) == 0 {
		return nil, fmt.Errorf("Claude output is empty")
	}
	if err := protocol.RejectDuplicateKeys(output); err != nil {
		return nil, err
	}
	var envelope struct {
		Type             string          `json:"type"`
		Subtype          string          `json:"subtype"`
		IsError          *bool           `json:"is_error"`
		StructuredOutput json.RawMessage `json:"structured_output"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, err
	}
	if envelope.Type != "result" || envelope.Subtype != "success" || envelope.IsError == nil || *envelope.IsError {
		return nil, fmt.Errorf("Claude did not report a successful result")
	}
	structured := bytes.TrimSpace(envelope.StructuredOutput)
	if len(structured) == 0 || structured[0] != '{' {
		return nil, fmt.Errorf("Claude result is missing structured_output object")
	}
	return append([]byte(nil), structured...), nil
}

func setEnvironmentValue(environment []string, name, value string) []string {
	updated := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if found && strings.EqualFold(key, name) {
			continue
		}
		updated = append(updated, entry)
	}
	return append(updated, name+"="+value)
}
