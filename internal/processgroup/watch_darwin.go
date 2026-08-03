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
		if errors.Is(err, unix.ESRCH) {
			return nil
		}
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

func ignoreCleanupErrorAfterExit(leaderPID int, err error) bool {
	if !errors.Is(err, unix.EPERM) {
		return false
	}
	for range 10 {
		safe, pending := cleanupStateAfterExit(leaderPID)
		if safe {
			return true
		}
		if !pending {
			return false
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

func cleanupStateAfterExit(leaderPID int) (safe, pending bool) {
	processes, queryErr := unix.SysctlKinfoProcSlice("kern.proc.pgrp", leaderPID)
	if queryErr != nil {
		return false, false
	}
	if containsOnlyZombieLeader(processes, leaderPID) {
		return true, false
	}
	if len(processes) == 1 && int(processes[0].Proc.P_pid) == leaderPID {
		return false, true
	}
	if len(processes) != 0 {
		return false, false
	}
	leader, queryErr := unix.SysctlKinfoProc("kern.proc.pid", leaderPID)
	if queryErr != nil {
		return false, false
	}
	if isZombieLeader(leader, leaderPID) {
		return true, false
	}
	return false, int(leader.Proc.P_pid) == leaderPID
}

func containsOnlyZombieLeader(processes []unix.KinfoProc, leaderPID int) bool {
	return len(processes) == 1 &&
		isZombieLeader(&processes[0], leaderPID)
}

func isZombieLeader(process *unix.KinfoProc, leaderPID int) bool {
	// Darwin's SZOMB value is 5 in sys/proc.h but is not exported by x/sys.
	const zombieState = 5
	return process != nil &&
		int(process.Proc.P_pid) == leaderPID &&
		process.Proc.P_stat == zombieState
}
