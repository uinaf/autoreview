package trustedexec

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestResolveSkipsRepositoryExecutableAndUsesExternalInstall(t *testing.T) {
	t.Parallel()

	repository := testRepository(t)
	repositoryBin := filepath.Join(repository, "bin")
	externalBin := t.TempDir()
	writeExecutable(t, filepath.Join(repositoryBin, "git"))
	external := writeExecutable(t, filepath.Join(externalBin, "git"))

	resolved, err := Resolve("git", "", repository, []string{"PATH=" + strings.Join([]string{repositoryBin, externalBin}, string(os.PathListSeparator))})
	if err != nil {
		t.Fatal(err)
	}
	if resolved != external {
		t.Fatalf("resolved executable = %q, want %q", resolved, external)
	}
}

func TestResolveUsesOutermostRepositoryBoundary(t *testing.T) {
	t.Parallel()

	repository := testRepository(t)
	nested := filepath.Join(repository, "nested")
	if err := os.MkdirAll(filepath.Join(nested, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := writeExecutable(t, filepath.Join(repository, "bin", "trufflehog"))
	if _, err := Resolve("trufflehog", path, nested, nil); err == nil {
		t.Fatal("Resolve() accepted an executable in the outer reviewed repository")
	}
}

func TestResolveRejectsSymlinkAcrossRepositoryBoundary(t *testing.T) {
	t.Parallel()

	repository := testRepository(t)
	external := writeExecutable(t, filepath.Join(t.TempDir(), "tool"))
	repositoryLink := filepath.Join(repository, "tool")
	if err := os.Symlink(external, repositoryLink); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve("tool", repositoryLink, repository, nil); err == nil {
		t.Fatal("Resolve() accepted a repository-local symlink")
	}

	repositoryTool := writeExecutable(t, filepath.Join(repository, "bin", "tool"))
	externalLink := filepath.Join(t.TempDir(), "tool")
	if err := os.Symlink(repositoryTool, externalLink); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve("tool", externalLink, repository, nil); err == nil {
		t.Fatal("Resolve() accepted a symlink targeting the reviewed repository")
	}
}

func TestResolveRejectsCaseAliasOnCaseInsensitiveFilesystem(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repository := filepath.Join(root, "Repo")
	if err := os.MkdirAll(filepath.Join(repository, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := writeExecutable(t, filepath.Join(repository, "bin", "git"))
	alias := filepath.Join(root, "repo", "bin", "git")
	if _, err := os.Stat(alias); err != nil {
		t.Skip("filesystem is case-sensitive")
	}
	if _, err := Resolve("git", alias, repository, nil); err == nil {
		t.Fatalf("Resolve() accepted case alias %q for %q", alias, path)
	}
}

func TestResolveSkipsRelativePATHEntries(t *testing.T) {
	t.Parallel()

	repository := testRepository(t)
	writeExecutable(t, filepath.Join(t.TempDir(), "git"))
	if _, err := Resolve("git", "", repository, []string{"PATH=relative/bin"}); err == nil {
		t.Fatal("Resolve() accepted a relative PATH entry")
	}
}

func TestResolveRequiresGitWorktreeBoundary(t *testing.T) {
	t.Parallel()

	external := writeExecutable(t, filepath.Join(t.TempDir(), "git"))
	if _, err := Resolve("git", external, t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "not inside a Git worktree") {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestGitEnvironmentIsMinimal(t *testing.T) {
	t.Parallel()

	environment := GitEnvironment()
	for _, name := range []string{"PATH", "HOME", "SSH_AUTH_SOCK", "GITHUB_TOKEN", "OPENAI_API_KEY"} {
		if slices.ContainsFunc(environment, func(entry string) bool { return strings.HasPrefix(entry, name+"=") }) {
			t.Fatalf("GitEnvironment() retained %s: %v", name, environment)
		}
	}
	for _, required := range []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0", "LANG=C", "LC_ALL=C"} {
		if !slices.Contains(environment, required) {
			t.Fatalf("GitEnvironment() missing %q: %v", required, environment)
		}
	}
}

func testRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	if err := os.Mkdir(filepath.Join(repository, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	return repository
}

func writeExecutable(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
