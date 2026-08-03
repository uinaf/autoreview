package target

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uinaf/autoreview/internal/trustedexec"
)

const diagnosticLimit = 8 << 10

var hardenedGitConfig = []string{
	"-c", "core.hooksPath=/dev/null",
	"-c", "core.fsmonitor=false",
	"-c", "color.ui=never",
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

func newGitClient(ctx context.Context, path, repository string) (*gitClient, error) {
	absolute, err := trustedexec.Resolve(
		ctx,
		"git",
		path,
		repository,
		os.Environ(),
		trustedexec.GitProbe(os.TempDir()),
	)
	if err != nil {
		return nil, fmt.Errorf("find git: %w", err)
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

func (git *gitClient) runSandboxTo(ctx context.Context, directory string, sandbox *gitSandbox, input []byte, output io.Writer, arguments ...string) error {
	return git.runConfiguredTo(ctx, directory, input, output, "", sandbox.environment, arguments...)
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
	if len(arguments) == 0 {
		return fmt.Errorf("git command requires a subcommand")
	}
	absoluteDirectory, err := filepath.Abs(directory)
	if err != nil {
		return fmt.Errorf("resolve git working directory: %w", err)
	}
	args := []string{"-C", absoluteDirectory}
	args = append(args, hardenedGitConfig...)
	if attributeSource != "" {
		args = append(args, "--attr-source="+attributeSource)
	}
	args = append(args, arguments...)
	command := exec.CommandContext(ctx, git.path, args...)
	command.Dir = os.TempDir()
	command.Env = append(hardenedEnvironment(), extraEnvironment...)
	command.Stdin = bytes.NewReader(input)
	command.WaitDelay = 2 * time.Second
	stderr := newLimitBuffer(diagnosticLimit)
	command.Stdout = stdout
	command.Stderr = stderr
	err = command.Run()
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
	return trustedexec.GitEnvironment()
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
		cut := 2000
		for cut > 0 && !utf8.RuneStart(value[cut]) {
			cut--
		}
		value = value[:cut]
	}
	return value
}
