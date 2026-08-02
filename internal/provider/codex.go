package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uinaf/autoreview/internal/config"
	"github.com/uinaf/autoreview/internal/protocol"
	contractschema "github.com/uinaf/autoreview/schema"
)

const (
	defaultExecutable       = "codex"
	probeOutputLimit        = int64(256 << 10)
	providerStdoutLimit     = int64(2 << 20)
	providerStderrLimit     = int64(256 << 10)
	providerResultLimit     = int64(1 << 20)
	providerPromptAllowance = int64(256 << 10)
)

var codexVersionPattern = regexp.MustCompile(`\b(\d+\.\d+\.\d+)\b`)

type CodexOptions struct {
	Repository  string
	Executable  string
	Environment []string
}

type Codex struct {
	repository  string
	executable  string
	environment []string
}

func NewCodex(options CodexOptions) *Codex {
	executable := options.Executable
	if executable == "" {
		executable = defaultExecutable
	}
	environment := options.Environment
	if environment == nil {
		environment = os.Environ()
	}
	return &Codex{
		repository:  options.Repository,
		executable:  executable,
		environment: append([]string(nil), environment...),
	}
}

func (codex *Codex) Review(ctx context.Context, request Request) (result Result, returnError error) {
	started := time.Now()
	if err := request.Config.Validate(); err != nil {
		return Result{}, newFailure(protocol.FailureConfig, fmt.Sprintf("invalid provider config: %v", err), nil, nil)
	}
	if request.Config.Engine.Value != protocol.ProviderCodex {
		return Result{}, newFailure(protocol.FailureConfig, fmt.Sprintf("Codex adapter cannot run engine %q", request.Config.Engine.Value), nil, nil)
	}
	if !utf8.ValidString(request.Prompt) || strings.TrimSpace(request.Prompt) == "" {
		return Result{}, newFailure(protocol.FailureConfig, "provider prompt must be non-empty valid UTF-8", nil, nil)
	}
	maximumPrompt := request.Config.MaxBytes.Value + providerPromptAllowance
	if maximumPrompt < request.Config.MaxBytes.Value || int64(len(request.Prompt)) > maximumPrompt {
		return Result{}, newFailure(protocol.FailureConfig, fmt.Sprintf("provider prompt exceeds %d bytes", maximumPrompt), nil, nil)
	}
	repository, err := filepath.Abs(codex.repository)
	if err != nil {
		return Result{}, newFailure(protocol.FailureConfig, fmt.Sprintf("resolve reviewed repository: %v", err), nil, nil)
	}
	executable, err := discoverExecutable(codex.executable, repository, codex.environment)
	if err != nil {
		return Result{}, newFailure(protocol.FailureCapability, err.Error(), codex.environment, nil)
	}
	runtime, err := config.PrepareRuntime(request.Config, codex.environment)
	if err != nil {
		return Result{}, newFailure(protocol.FailureInternal, fmt.Sprintf("prepare provider runtime: %v", err), codex.environment, nil)
	}
	defer func() {
		if err := runtime.Close(); err != nil && returnError == nil {
			result = Result{}
			returnError = newFailure(protocol.FailureInternal, err.Error(), runtime.Environment(), nil)
		}
	}()
	version, err := codex.preflight(ctx, executable, runtime, request.Config)
	if err != nil {
		return Result{}, err
	}
	state, err := os.MkdirTemp("", "autoreview-codex-state-")
	if err != nil {
		return Result{}, newFailure(protocol.FailureInternal, fmt.Sprintf("create Codex state: %v", err), runtime.Environment(), nil)
	}
	defer func() {
		if err := os.RemoveAll(state); err != nil && returnError == nil {
			result = Result{}
			returnError = newFailure(protocol.FailureInternal, fmt.Sprintf("remove Codex state: %v", err), runtime.Environment(), nil)
		}
	}()
	schemaPath := filepath.Join(state, "review-v1.schema.json")
	outputPath := filepath.Join(state, "last-message.json")
	codexSchema, err := contractschema.CodexReviewV1()
	if err != nil {
		return Result{}, newFailure(protocol.FailureInternal, err.Error(), runtime.Environment(), nil)
	}
	if err := os.WriteFile(schemaPath, codexSchema, 0o600); err != nil {
		return Result{}, newFailure(protocol.FailureInternal, fmt.Sprintf("write Codex schema: %v", err), runtime.Environment(), nil)
	}
	if err := os.WriteFile(outputPath, nil, 0o600); err != nil {
		return Result{}, newFailure(protocol.FailureInternal, fmt.Sprintf("create Codex output: %v", err), runtime.Environment(), nil)
	}
	model := request.Config.Model.Value
	if model == "" {
		model = DefaultCodexModel
	}
	arguments := codexArguments(request.Config, runtime.Workspace, schemaPath, outputPath, model)
	process, processErr := runProcess(ctx, processSpec{
		Path:        executable,
		Arguments:   arguments,
		Directory:   runtime.Workspace,
		Environment: runtime.Environment(),
		Input:       []byte(request.Prompt),
		Timeout:     time.Duration(request.Config.Timeout.Value),
		StdoutLimit: providerStdoutLimit,
		StderrLimit: providerStderrLimit,
	})
	attempt := protocol.Attempt{Number: 1, DurationMS: process.Duration.Milliseconds()}
	if processErr != nil {
		class := classifyProcessFailure(processErr, process)
		attempt.Outcome = protocol.AttemptFailed
		attempt.ErrorClass = &class
		return Result{}, processFailure("Codex review", class, processErr, process, runtime.Environment(), &attempt)
	}
	message, err := decodeCodexEnvelope(process.Stdout)
	if err != nil {
		class := protocol.FailureProtocol
		attempt.Outcome = protocol.AttemptMalformed
		attempt.ErrorClass = &class
		return Result{}, newFailure(class, fmt.Sprintf("decode Codex envelope: %v", err), runtime.Environment(), &attempt)
	}
	output, err := readProviderResult(outputPath)
	if err != nil {
		class := protocol.FailureProtocol
		attempt.Outcome = protocol.AttemptMalformed
		attempt.ErrorClass = &class
		return Result{}, newFailure(class, fmt.Sprintf("read Codex result: %v", err), runtime.Environment(), &attempt)
	}
	if !bytes.Equal(bytes.TrimSpace(output), bytes.TrimSpace([]byte(message))) {
		class := protocol.FailureProtocol
		attempt.Outcome = protocol.AttemptMalformed
		attempt.ErrorClass = &class
		return Result{}, newFailure(class, "Codex envelope and last-message output disagree", runtime.Environment(), &attempt)
	}
	review, err := protocol.DecodeReview(output)
	if err != nil {
		class := protocol.FailureProtocol
		attempt.Outcome = protocol.AttemptMalformed
		attempt.ErrorClass = &class
		return Result{}, newFailure(class, fmt.Sprintf("decode Codex review: %v", err), runtime.Environment(), &attempt)
	}
	attempt.Outcome = protocol.AttemptValid
	return Result{
		Review: review,
		Provider: protocol.Provider{
			Name:    protocol.ProviderCodex,
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

func (codex *Codex) preflight(ctx context.Context, executable string, runtime *config.Runtime, effective config.Effective) (string, error) {
	timeout := 10 * time.Second
	if time.Duration(effective.Timeout.Value) < timeout {
		timeout = time.Duration(effective.Timeout.Value)
	}
	run := func(arguments ...string) (processResult, error) {
		return runProcess(ctx, processSpec{
			Path:        executable,
			Arguments:   arguments,
			Directory:   runtime.Workspace,
			Environment: runtime.Environment(),
			Timeout:     timeout,
			StdoutLimit: probeOutputLimit,
			StderrLimit: probeOutputLimit,
		})
	}
	versionResult, err := run("--version")
	if err != nil {
		return "", probeFailure("Codex --version", err, versionResult, runtime.Environment(), protocol.FailureCapability)
	}
	match := codexVersionPattern.FindSubmatch(append(versionResult.Stdout, versionResult.Stderr...))
	if len(match) != 2 {
		return "", newFailure(protocol.FailureCapability, "Codex --version did not report a semantic version", runtime.Environment(), nil)
	}
	topHelp, err := run("--help")
	if err != nil {
		return "", probeFailure("Codex --help", err, topHelp, runtime.Environment(), protocol.FailureCapability)
	}
	requiredTop := []string{"--ask-for-approval"}
	if effective.Isolation.Value == protocol.IsolationStrict {
		requiredTop = append(requiredTop, "--strict-config")
	}
	if effective.WebAccess.Value {
		requiredTop = append(requiredTop, "--search")
	}
	if missing := missingCapabilities(string(topHelp.Stdout)+string(topHelp.Stderr), requiredTop); len(missing) != 0 {
		return "", newFailure(protocol.FailureCapability, "Codex is missing required top-level flags: "+strings.Join(missing, ", "), runtime.Environment(), nil)
	}
	execHelp, err := run("exec", "--help")
	if err != nil {
		return "", probeFailure("Codex exec --help", err, execHelp, runtime.Environment(), protocol.FailureCapability)
	}
	requiredExec := []string{"--ephemeral", "--skip-git-repo-check", "--output-schema", "--output-last-message", "--json", "--cd"}
	if effective.Isolation.Value == protocol.IsolationStrict {
		requiredExec = append(requiredExec, "--ignore-user-config", "--ignore-rules", "--sandbox")
	}
	if missing := missingCapabilities(string(execHelp.Stdout)+string(execHelp.Stderr), requiredExec); len(missing) != 0 {
		return "", newFailure(protocol.FailureCapability, "Codex exec is missing required flags: "+strings.Join(missing, ", "), runtime.Environment(), nil)
	}
	if environmentValue(runtime.Environment(), "OPENAI_API_KEY") == "" {
		auth, err := run("login", "status")
		if err != nil {
			return "", probeFailure("Codex authentication", err, auth, runtime.Environment(), protocol.FailureAuth)
		}
	}
	return string(match[1]), nil
}

func codexArguments(effective config.Effective, workspace, schemaPath, outputPath, model string) []string {
	arguments := []string{
		"--ask-for-approval", "never",
		"--model", model,
		"-c", fmt.Sprintf("model_reasoning_effort=%q", effective.ReasoningEffort.Value),
	}
	if effective.WebAccess.Value {
		arguments = append(arguments, "--search")
	} else {
		arguments = append(arguments, "-c", `web_search="disabled"`)
	}
	if effective.Isolation.Value == protocol.IsolationStrict {
		arguments = append(arguments,
			"--strict-config",
			"-c", "project_doc_max_bytes=0",
			"-c", "features.shell_snapshot=false",
			"-c", "features.hooks=false",
			"-c", "features.plugins=false",
			"-c", "features.multi_agent=false",
			"-c", "skills.include_instructions=false",
			"-c", "skills.config=[]",
			"-c", `shell_environment_policy.inherit="core"`,
			"-c", "shell_environment_policy.ignore_default_excludes=false",
			"-c", `shell_environment_policy.set={GIT_CONFIG_GLOBAL="/dev/null",GIT_CONFIG_SYSTEM="/dev/null",GIT_TERMINAL_PROMPT="0"}`,
			"-c", "shell_environment_policy.experimental_use_profile=false",
			"-c", "allow_login_shell=false",
			"-c", `default_permissions="autoreview"`,
			"-c", `permissions.autoreview.filesystem={":minimal"="read",":workspace_roots"="read"}`,
		)
	}
	arguments = append(arguments, "exec", "--json", "--color", "never", "--ephemeral", "--skip-git-repo-check", "-C", workspace)
	if effective.Isolation.Value == protocol.IsolationStrict {
		arguments = append(arguments, "--sandbox", "read-only", "--ignore-user-config", "--ignore-rules")
	}
	arguments = append(arguments, "--output-schema", schemaPath, "--output-last-message", outputPath, "-")
	return arguments
}

func decodeCodexEnvelope(output []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64<<10), int(providerStdoutLimit))
	threadStarted := false
	turnStarted := false
	turnCompleted := false
	lastMessage := ""
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if turnCompleted {
			return "", fmt.Errorf("Codex emitted an event after turn.completed")
		}
		if err := protocol.RejectDuplicateKeys(line); err != nil {
			return "", err
		}
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Message  string `json:"message"`
			Error    any    `json:"error"`
			Item     *struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return "", err
		}
		switch event.Type {
		case "thread.started":
			if threadStarted || strings.TrimSpace(event.ThreadID) == "" {
				return "", fmt.Errorf("invalid thread.started event")
			}
			threadStarted = true
		case "turn.started":
			if !threadStarted || turnStarted {
				return "", fmt.Errorf("invalid turn.started event")
			}
			turnStarted = true
		case "item.completed":
			if !turnStarted || event.Item == nil {
				return "", fmt.Errorf("invalid item.completed event")
			}
			if event.Item.Type == "agent_message" {
				lastMessage = event.Item.Text
			}
		case "turn.completed":
			if !turnStarted || turnCompleted {
				return "", fmt.Errorf("invalid turn.completed event")
			}
			turnCompleted = true
		case "error", "turn.failed":
			detail := event.Message
			if detail == "" && event.Error != nil {
				encoded, _ := json.Marshal(event.Error)
				detail = string(encoded)
			}
			return "", fmt.Errorf("Codex reported %s: %s", event.Type, detail)
		case "":
			return "", fmt.Errorf("Codex event is missing type")
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if !threadStarted || !turnStarted || !turnCompleted || strings.TrimSpace(lastMessage) == "" {
		return "", fmt.Errorf("incomplete Codex JSONL envelope")
	}
	return lastMessage, nil
}

func readProviderResult(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("result is not a regular file")
	}
	if info.Size() > providerResultLimit {
		return nil, fmt.Errorf("result exceeds %d bytes", providerResultLimit)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > providerResultLimit {
		return nil, fmt.Errorf("result exceeds %d bytes", providerResultLimit)
	}
	return content, nil
}

func classifyProcessFailure(err error, result processResult) protocol.FailureClass {
	processFailure := new(processError)
	if errors.As(err, &processFailure) {
		switch processFailure.Kind {
		case processTimeout:
			return protocol.FailureTimeout
		case processCancelled:
			return protocol.FailureCancelled
		}
	}
	detail := strings.ToLower(string(result.Stderr) + "\n" + string(result.Stdout))
	for _, marker := range []string{"not logged in", "unauthorized", "authentication", "login required", "401"} {
		if strings.Contains(detail, marker) {
			return protocol.FailureAuth
		}
	}
	return protocol.FailureProvider
}

func processFailure(operation string, class protocol.FailureClass, err error, result processResult, environment []string, attempt *protocol.Attempt) *Error {
	details := make([]string, 0, 2)
	if strings.TrimSpace(string(result.Stderr)) != "" {
		details = append(details, "stderr:\n"+string(result.Stderr))
	}
	if strings.TrimSpace(string(result.Stdout)) != "" {
		details = append(details, "stdout:\n"+string(result.Stdout))
	}
	message := fmt.Sprintf("%s failed: %v", operation, err)
	if len(details) != 0 {
		message += ":\n" + strings.Join(details, "\n")
	}
	return newFailure(class, message, environment, attempt)
}

func probeFailure(operation string, err error, result processResult, environment []string, fallback protocol.FailureClass) *Error {
	class := classifyProcessFailure(err, result)
	if class == protocol.FailureProvider {
		class = fallback
	}
	return processFailure(operation, class, err, result, environment, nil)
}

func newFailure(class protocol.FailureClass, message string, environment []string, attempt *protocol.Attempt) *Error {
	return &Error{Class: class, Message: sanitizeDiagnostic(message, environment), Attempt: attempt}
}

func missingCapabilities(output string, required []string) []string {
	missing := make([]string, 0)
	for _, capability := range required {
		if !strings.Contains(output, capability) {
			missing = append(missing, capability)
		}
	}
	return missing
}
