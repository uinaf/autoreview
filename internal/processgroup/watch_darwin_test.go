//go:build darwin

package processgroup

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestContainsOnlyZombieLeader(t *testing.T) {
	t.Parallel()
	leader := unix.KinfoProc{}
	leader.Proc.P_pid = 42
	leader.Proc.P_stat = 5
	liveChild := unix.KinfoProc{}
	liveChild.Proc.P_pid = 43
	liveChild.Proc.P_stat = 2

	tests := []struct {
		name      string
		processes []unix.KinfoProc
		want      bool
	}{
		{name: "zombie leader only", processes: []unix.KinfoProc{leader}, want: true},
		{name: "live leader", processes: []unix.KinfoProc{{Proc: unix.ExternProc{P_pid: 42, P_stat: 2}}}},
		{name: "different zombie", processes: []unix.KinfoProc{{Proc: unix.ExternProc{P_pid: 41, P_stat: 5}}}},
		{name: "live descendant", processes: []unix.KinfoProc{leader, liveChild}},
		{name: "empty group"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := containsOnlyZombieLeader(test.processes, 42); got != test.want {
				t.Fatalf("containsOnlyZombieLeader() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestIsZombieLeader(t *testing.T) {
	t.Parallel()

	zombie := unix.KinfoProc{}
	zombie.Proc.P_pid = 42
	zombie.Proc.P_stat = 5
	live := zombie
	live.Proc.P_stat = 2
	if !isZombieLeader(&zombie, 42) {
		t.Fatal("isZombieLeader() rejected the expected zombie")
	}
	if isZombieLeader(&live, 42) || isZombieLeader(&zombie, 41) || isZombieLeader(nil, 42) {
		t.Fatal("isZombieLeader() accepted a non-matching process")
	}
}
