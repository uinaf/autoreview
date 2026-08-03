//go:build darwin || linux

package trustedexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/uinaf/autoreview/internal/processgroup"
)

func Probe(arguments []string, directory string, environment []string) Check {
	return func(path string) error {
		return runProbe(path, arguments, directory, environment, io.Discard)
	}
}

func GitProbe(directory string) Check {
	return func(path string) error {
		output := &probeBuffer{limit: 4 << 10}
		if err := runProbe(path, []string{"-C", directory, "--version"}, directory, GitEnvironment(), output); err != nil {
			return err
		}
		if output.exceeded {
			return newProbeCapabilityError("git version output exceeded safe limit", nil)
		}
		if err := requireGitVersion(output.String()); err != nil {
			return newProbeCapabilityError(err.Error(), err)
		}
		return nil
	}
}

type probeCapabilityError struct {
	message string
	cause   error
}

func newProbeCapabilityError(message string, cause error) *probeCapabilityError {
	return &probeCapabilityError{message: message, cause: cause}
}

func (err *probeCapabilityError) Error() string {
	return err.message
}

func (err *probeCapabilityError) Unwrap() error {
	return err.cause
}

func probeDiagnostic(err error) (string, bool) {
	var capability *probeCapabilityError
	if !errors.As(err, &capability) {
		return "", false
	}
	return capability.message, capability.message != ""
}

type probeCleanupError struct {
	cause error
}

func (err *probeCleanupError) Error() string {
	return "capability probe process cleanup failed"
}

func (err *probeCleanupError) Unwrap() error {
	return err.cause
}

func isProbeCleanupError(err error) bool {
	var cleanup *probeCleanupError
	return errors.As(err, &cleanup)
}

func runProbe(path string, arguments []string, directory string, environment []string, stdout io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	//nolint:noctx // processgroup.Run owns cancellation so it can kill the group before reaping the leader.
	command := exec.Command(path, arguments...)
	command.Dir = directory
	command.Env = append([]string(nil), environment...)
	command.Stdout = stdout
	command.Stderr = io.Discard
	result := processgroup.Run(ctx, command)
	if result.CleanupErr != nil {
		return &probeCleanupError{cause: result.CleanupErr}
	}
	if result.CommandErr == nil {
		return nil
	}
	message := "capability probe failed"
	switch {
	case errors.Is(result.CommandErr, context.DeadlineExceeded):
		message = "capability probe timed out"
	case errors.Is(result.CommandErr, context.Canceled):
		message = "capability probe was cancelled"
	default:
		var exitError *exec.ExitError
		if errors.As(result.CommandErr, &exitError) {
			message = fmt.Sprintf("capability probe exited with status %d", exitError.ExitCode())
		}
	}
	return newProbeCapabilityError(message, result.CommandErr)
}

func requireGitVersion(output string) error {
	const prefix = "git version "
	value := strings.TrimSpace(output)
	if !strings.HasPrefix(value, prefix) {
		return errors.New("git returned an unrecognized version string")
	}
	fields := strings.Fields(strings.TrimPrefix(value, prefix))
	if len(fields) == 0 {
		return errors.New("git returned an unrecognized version string")
	}
	components := strings.Split(fields[0], ".")
	if len(components) < 2 {
		return errors.New("git returned an unrecognized version string")
	}
	major, majorErr := strconv.Atoi(components[0])
	minor, minorErr := strconv.Atoi(components[1])
	if majorErr != nil || minorErr != nil {
		return errors.New("git returned an unrecognized version string")
	}
	if major < 2 || major == 2 && minor < 41 {
		return fmt.Errorf("git 2.41 or newer is required; found %d.%d", major, minor)
	}
	return nil
}

type probeBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (buffer *probeBuffer) Write(data []byte) (int, error) {
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining <= 0 {
		buffer.exceeded = true
		return len(data), nil
	}
	write := len(data)
	if write > remaining {
		write = remaining
		buffer.exceeded = true
	}
	_, _ = buffer.buffer.Write(data[:write])
	return len(data), nil
}

func (buffer *probeBuffer) String() string {
	return buffer.buffer.String()
}
