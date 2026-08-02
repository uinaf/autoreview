package buildinfo

import "testing"

func TestVersion(t *testing.T) {
	got := Version()
	want := "autoreview dev (unknown)"
	if got != want {
		t.Fatalf("Version() = %q, want %q", got, want)
	}
}
