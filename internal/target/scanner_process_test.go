package target

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestTruffleHogScannerCancellationKillsDescendants(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "survived")
	childPID := filepath.Join(t.TempDir(), "child.pid")
	script := filepath.Join(t.TempDir(), "trufflehog")
	writeFile(t, filepath.Dir(script), filepath.Base(script), "#!/bin/sh\nfor last in \"$@\"; do :; done\nif [ ! -e \"$last/frozen-review.txt\" ]; then exit 0; fi\n(/bin/sleep 0.25; printf survived > "+quoteShellTest(marker)+") &\nprintf '%s' \"$!\" > "+quoteShellTest(childPID)+"\nwait\n")
	if err := os.Chmod(script, 0o700); err != nil {
		t.Fatal(err)
	}
	scanner, err := newTruffleHogScanner(script, repositoryBoundaryFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- scanner.Scan(ctx, []byte("benign"))
	}()
	pid := waitForChildPID(t, childPID)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Scan() error = %v, want context cancellation", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("scanner did not return after cancellation")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("scanner descendant %d survived cancellation", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
	assertScannerMarkerNotWritten(t, marker)
}

func TestTruffleHogScannerCleansDescendantsAfterLeaderExit(t *testing.T) {
	tests := []struct {
		name       string
		exitStatus int
	}{
		{name: "success"},
		{name: "failure", exitStatus: 7},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "survived")
			script := filepath.Join(t.TempDir(), "trufflehog")
			writeFile(t, filepath.Dir(script), filepath.Base(script), "#!/bin/sh\nfor last in \"$@\"; do :; done\nif [ ! -e \"$last/frozen-review.txt\" ]; then exit 0; fi\n(/bin/sleep 0.25; printf survived > "+quoteShellTest(marker)+") &\nexit "+strconv.Itoa(test.exitStatus)+"\n")
			if err := os.Chmod(script, 0o700); err != nil {
				t.Fatal(err)
			}
			scanner, err := newTruffleHogScanner(script, repositoryBoundaryFixture(t))
			if err != nil {
				t.Fatal(err)
			}
			err = scanner.Scan(context.Background(), []byte("benign"))
			if test.exitStatus == 0 {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "trufflehog scan failed") {
				t.Fatalf("Scan() error = %v", err)
			}
			assertScannerMarkerNotWritten(t, marker)
		})
	}
}

func waitForChildPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		content, err := os.ReadFile(path)
		if err == nil && len(content) != 0 {
			pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
			if err != nil {
				t.Fatal(err)
			}
			return pid
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("scanner descendant did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func quoteShellTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func assertScannerMarkerNotWritten(t *testing.T, marker string) {
	t.Helper()
	time.Sleep(400 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scanner descendant wrote marker: %v", err)
	}
}
