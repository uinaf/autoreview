//go:build darwin || linux

package processgroup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

type Result struct {
	CommandErr error
	CleanupErr error
}

func (result Result) Err() error {
	return errors.Join(result.CommandErr, result.CleanupErr)
}

func Run(ctx context.Context, command *exec.Cmd) Result {
	if err := ctx.Err(); err != nil {
		return Result{CommandErr: err}
	}
	configure(command)
	command.WaitDelay = 2 * time.Second
	if err := command.Start(); err != nil {
		return Result{CommandErr: err}
	}

	// Observe exit without reaping so the leader PID cannot be reused before
	// its process group is terminated.
	watchErr := waitForExit(ctx, command.Process.Pid)
	cleanupErr := terminate(command.Process.Pid)
	if watchErr == nil && ignoreCleanupErrorAfterExit(command.Process.Pid, cleanupErr) {
		cleanupErr = nil
	}
	if watchErr != nil {
		if leaderErr := command.Process.Kill(); leaderErr != nil && !errors.Is(leaderErr, os.ErrProcessDone) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("terminate process-group leader: %w", leaderErr))
		}
	}
	waitErr := command.Wait()
	if errors.Is(waitErr, exec.ErrWaitDelay) {
		waitErr = fmt.Errorf("wait for process-group I/O; output may be incomplete: %w", waitErr)
	}
	if watchErr == nil {
		watchErr = ctx.Err()
	}
	if watchErr != nil && !errors.Is(watchErr, context.Canceled) && !errors.Is(watchErr, context.DeadlineExceeded) {
		watchErr = fmt.Errorf("watch process-group leader: %w", watchErr)
	}
	if cleanupErr != nil {
		cleanupErr = fmt.Errorf("terminate process group: %w", cleanupErr)
	}
	return Result{
		CommandErr: errors.Join(watchErr, waitErr),
		CleanupErr: cleanupErr,
	}
}
