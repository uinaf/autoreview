package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverExecutableRejectsReviewedRepository(t *testing.T) {
	t.Parallel()

	repository := t.TempDir()
	path := filepath.Join(repository, "bin", "codex")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverExecutable(path, repository, []string{"PATH=" + filepath.Dir(path)}); err == nil {
		t.Fatal("discoverExecutable() accepted a repository-local executable")
	}
	if _, err := discoverExecutable("codex", repository, []string{"PATH=" + filepath.Dir(path)}); err == nil {
		t.Fatal("discoverExecutable() accepted a repository-local PATH entry")
	}
}

func TestDiscoverExecutableResolvesExternalSymlink(t *testing.T) {
	t.Parallel()

	repository := t.TempDir()
	external := writeTestExecutable(t, "codex-real", "#!/bin/sh\nexit 0\n")
	link := filepath.Join(t.TempDir(), "codex")
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := discoverExecutable(link, repository, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(external)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != want {
		t.Fatalf("resolved executable = %q, want %q", resolved, want)
	}
}

func TestDiscoverExecutableRejectsRepositorySymlinkToExternalBinary(t *testing.T) {
	t.Parallel()

	repository := t.TempDir()
	external := writeTestExecutable(t, "codex-real", "#!/bin/sh\nexit 0\n")
	link := filepath.Join(repository, "codex")
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverExecutable(link, repository, nil); err == nil {
		t.Fatal("discoverExecutable() accepted a repository-local symlink")
	}
}

func TestDiscoverExecutableSkipsRelativePathEntries(t *testing.T) {
	t.Parallel()

	repository := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "codex"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(workingDirectory, outside)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := discoverExecutable("codex", repository, []string{"PATH=" + relative}); err == nil {
		t.Fatal("discoverExecutable() accepted a relative PATH entry")
	}
}
