package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

type processErrorKind string

const (
	processStart       processErrorKind = "start"
	processExit        processErrorKind = "exit"
	processTimeout     processErrorKind = "timeout"
	processCancelled   processErrorKind = "cancelled"
	processOutputLimit processErrorKind = "output_limit"
)

type processSpec struct {
	Path        string
	Arguments   []string
	Directory   string
	Environment []string
	Input       []byte
	Timeout     time.Duration
	StdoutLimit int64
	StderrLimit int64
}

type processResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Duration time.Duration
}

type processError struct {
	Kind   processErrorKind
	Result processResult
	Err    error
}

func (failure *processError) Error() string {
	return fmt.Sprintf("provider process %s: %v", failure.Kind, failure.Err)
}

func (failure *processError) Unwrap() error {
	return failure.Err
}

func runProcess(ctx context.Context, spec processSpec) (processResult, error) {
	if spec.Timeout <= 0 {
		return processResult{}, &processError{Kind: processStart, Err: fmt.Errorf("timeout must be positive")}
	}
	if spec.StdoutLimit < 0 || spec.StderrLimit < 0 {
		return processResult{}, &processError{Kind: processStart, Err: fmt.Errorf("output limits must not be negative")}
	}
	runContext, cancel := context.WithTimeout(ctx, spec.Timeout)
	defer cancel()
	command := exec.CommandContext(runContext, spec.Path, spec.Arguments...)
	command.Dir = spec.Directory
	command.Env = append(make([]string, 0, len(spec.Environment)), spec.Environment...)
	command.Stdin = bytes.NewReader(spec.Input)
	configureProcess(command)
	command.Cancel = func() error {
		return killProcessGroup(command.Process)
	}
	command.WaitDelay = 2 * time.Second
	var overflow atomic.Bool
	stdout := newBoundedBuffer(spec.StdoutLimit, func() {
		overflow.Store(true)
		cancel()
	})
	stderr := newBoundedBuffer(spec.StderrLimit, func() {
		overflow.Store(true)
		cancel()
	})
	command.Stdout = stdout
	command.Stderr = stderr
	started := time.Now()
	err := command.Run()
	result := processResult{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: 0,
		Duration: time.Since(started),
	}
	if err == nil {
		return result, nil
	}
	if exitError := new(exec.ExitError); errors.As(err, &exitError) {
		result.ExitCode = exitError.ExitCode()
	} else {
		result.ExitCode = -1
	}
	kind := processExit
	switch {
	case overflow.Load():
		kind = processOutputLimit
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		kind = processTimeout
	case ctx.Err() != nil:
		kind = processCancelled
	case errors.Is(runContext.Err(), context.DeadlineExceeded):
		kind = processTimeout
	case result.ExitCode == -1:
		kind = processStart
	}
	return result, &processError{Kind: kind, Result: result, Err: err}
}

type boundedBuffer struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	limit    int64
	overflow sync.Once
	onLimit  func()
}

func newBoundedBuffer(limit int64, onLimit func()) *boundedBuffer {
	return &boundedBuffer{limit: limit, onLimit: onLimit}
}

func (writer *boundedBuffer) Write(data []byte) (int, error) {
	written := len(data)
	writer.mu.Lock()
	remaining := writer.limit - int64(writer.buffer.Len())
	if remaining > 0 {
		keep := int64(len(data))
		if keep > remaining {
			keep = remaining
		}
		_, _ = writer.buffer.Write(data[:keep])
	}
	exceeded := int64(len(data)) > remaining
	writer.mu.Unlock()
	if exceeded {
		writer.overflow.Do(writer.onLimit)
	}
	return written, nil
}

func (writer *boundedBuffer) Bytes() []byte {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return append([]byte(nil), writer.buffer.Bytes()...)
}
