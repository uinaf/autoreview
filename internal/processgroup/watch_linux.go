//go:build linux

package processgroup

import (
	"context"
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

func waitForExit(ctx context.Context, pid int) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var info unix.Siginfo
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Signo != 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func ignoreCleanupErrorAfterExit(_ error) bool {
	return false
}
