//go:build darwin

package processgroup

import (
	"context"
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

func waitForExit(ctx context.Context, pid int) error {
	queue, err := unix.Kqueue()
	if err != nil {
		return err
	}
	defer unix.Close(queue)
	change := unix.Kevent_t{
		Ident:  uint64(pid),
		Filter: unix.EVFILT_PROC,
		Flags:  unix.EV_ADD | unix.EV_ENABLE | unix.EV_ONESHOT,
		Fflags: unix.NOTE_EXIT,
	}
	if _, err := unix.Kevent(queue, []unix.Kevent_t{change}, nil, nil); err != nil {
		return err
	}
	events := make([]unix.Kevent_t, 1)
	for {
		timeout := unix.NsecToTimespec((10 * time.Millisecond).Nanoseconds())
		count, err := unix.Kevent(queue, nil, events, &timeout)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func ignoreCleanupErrorAfterExit(err error) bool {
	// macOS reports EPERM when the group contains only its zombie leader.
	return errors.Is(err, unix.EPERM)
}
