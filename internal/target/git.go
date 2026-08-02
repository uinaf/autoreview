package target

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const diagnosticLimit = 8 << 10

var hardenedGitConfig = []string{
	"-c", "core.hooksPath=/dev/null",
	"-c", "core.fsmonitor=false",
	"-c", "core.autocrlf=false",
	"-c", "core.safecrlf=false",
	"-c", "core.quotePath=false",
	"-c", "credential.helper=",
	"-c", "diff.external=",
	"-c", "diff.renames=false",
	"-c", "filter.lfs.clean=",
	"-c", "filter.lfs.smudge=",
	"-c", "filter.lfs.process=",
	"-c", "filter.lfs.required=false",
}

type gitClient struct {
	path string
}

type gitSandbox struct {
	directory       string
	environment     []string
	attributeSource string
}

func (sandbox *gitSandbox) Close() error {
	return os.RemoveAll(sandbox.directory)
}

func newGitClient(path string) (*gitClient, error) {
	if path == "" {
		var err error
		path, err = exec.LookPath("git")
		if err != nil {
			return nil, fmt.Errorf("find git: %w", err)
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve git path: %w", err)
	}
	return &gitClient{path: absolute}, nil
}

func (git *gitClient) run(ctx context.Context, directory string, input []byte, outputLimit int64, arguments ...string) ([]byte, error) {
	return git.runConfigured(ctx, directory, input, outputLimit, "", nil, arguments...)
}

func (git *gitClient) runNoReplace(ctx context.Context, directory string, input []byte, outputLimit int64, arguments ...string) ([]byte, error) {
	return git.runConfigured(ctx, directory, input, outputLimit, "", []string{"GIT_NO_REPLACE_OBJECTS=1"}, arguments...)
}

func (git *gitClient) runSandbox(ctx context.Context, directory string, sandbox *gitSandbox, input []byte, outputLimit int64, arguments ...string) ([]byte, error) {
	return git.runConfigured(ctx, directory, input, outputLimit, "", sandbox.environment, arguments...)
}

func (git *gitClient) runSandboxWithAttributes(ctx context.Context, directory string, sandbox *gitSandbox, input []byte, outputLimit int64, arguments ...string) ([]byte, error) {
	if !validObjectID(sandbox.attributeSource) {
		return nil, fmt.Errorf("invalid attribute source object ID")
	}
	return git.runConfigured(ctx, directory, input, outputLimit, sandbox.attributeSource, sandbox.environment, arguments...)
}

func (git *gitClient) runSandboxWithAttributesTo(ctx context.Context, directory string, sandbox *gitSandbox, input []byte, output io.Writer, arguments ...string) error {
	if !validObjectID(sandbox.attributeSource) {
		return fmt.Errorf("invalid attribute source object ID")
	}
	return git.runConfiguredTo(ctx, directory, input, output, sandbox.attributeSource, sandbox.environment, arguments...)
}

func (git *gitClient) runConfigured(ctx context.Context, directory string, input []byte, outputLimit int64, attributeSource string, extraEnvironment []string, arguments ...string) ([]byte, error) {
	stdout := newLimitBuffer(outputLimit)
	err := git.runConfiguredTo(ctx, directory, input, stdout, attributeSource, extraEnvironment, arguments...)
	if err != nil {
		return nil, err
	}
	if stdout.exceeded {
		return stdout.Bytes(), &outputLimitError{limit: outputLimit}
	}
	return stdout.Bytes(), nil
}

func (git *gitClient) runConfiguredTo(ctx context.Context, directory string, input []byte, stdout io.Writer, attributeSource string, extraEnvironment []string, arguments ...string) error {
	args := append(append([]string(nil), hardenedGitConfig...), arguments...)
	if attributeSource != "" {
		args = append(append(append([]string(nil), hardenedGitConfig...), "--attr-source="+attributeSource), arguments...)
	}
	command := exec.CommandContext(ctx, git.path, args...)
	command.Dir = directory
	command.Env = append(hardenedEnvironment(), extraEnvironment...)
	command.Stdin = bytes.NewReader(input)
	command.WaitDelay = 2 * time.Second
	stderr := newLimitBuffer(diagnosticLimit)
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if stderr.exceeded {
		return fmt.Errorf("git %s diagnostic output exceeded safe limit", arguments[0])
	}
	if err != nil {
		diagnostic := strings.TrimSpace(stderr.String())
		if diagnostic == "" {
			diagnostic = err.Error()
		}
		return fmt.Errorf("git %s: %s", arguments[0], sanitizeDiagnostic(diagnostic))
	}
	return nil
}

func validObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func hardenedEnvironment() []string {
	environment := make([]string, 0, len(os.Environ())+7)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "GIT_") || upper == "LANG" || strings.HasPrefix(upper, "LC_") || upper == "DYLD_INSERT_LIBRARIES" || upper == "LD_PRELOAD" || upper == "TSAN_OPTIONS" {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_ATTR_NOSYSTEM=1",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0",
		"LANG=C",
		"LC_ALL=C",
	)
}

type outputLimitError struct {
	limit int64
}

func (err *outputLimitError) Error() string {
	return fmt.Sprintf("command output exceeded %d bytes", err.limit)
}

type limitBuffer struct {
	buffer   bytes.Buffer
	limit    int64
	total    int64
	exceeded bool
}

func newLimitBuffer(limit int64) *limitBuffer {
	if limit < 1 {
		limit = 1
	}
	return &limitBuffer{limit: limit}
}

func (buffer *limitBuffer) Write(data []byte) (int, error) {
	buffer.total += int64(len(data))
	remaining := buffer.limit - int64(buffer.buffer.Len())
	if remaining <= 0 {
		buffer.exceeded = true
		return len(data), nil
	}
	write := int64(len(data))
	if write > remaining {
		write = remaining
		buffer.exceeded = true
	}
	_, _ = buffer.buffer.Write(data[:write])
	return len(data), nil
}

func (buffer *limitBuffer) Bytes() []byte {
	return append([]byte(nil), buffer.buffer.Bytes()...)
}

func (buffer *limitBuffer) String() string {
	return buffer.buffer.String()
}

func (buffer *limitBuffer) WriteString(value string) (int, error) {
	return buffer.Write([]byte(value))
}

func sanitizeDiagnostic(value string) string {
	value = strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7f {
			return ' '
		}
		return character
	}, value)
	if len(value) > 2000 {
		value = value[:2000]
	}
	return value
}

func isOutputLimit(err error) bool {
	var limitErr *outputLimitError
	return errors.As(err, &limitErr)
}
