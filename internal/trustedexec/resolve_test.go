package trustedexec

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestResolveSkipsRepositoryExecutableAndUsesExternalInstall(t *testing.T) {
	t.Parallel()

	repository := testRepository(t)
	repositoryBin := filepath.Join(repository, "bin")
	externalBin := t.TempDir()
	writeExecutable(t, filepath.Join(repositoryBin, "git"))
	external := writeExecutable(t, filepath.Join(externalBin, "git"))

	resolved, err := Resolve("git", "", repository, []string{"PATH=" + strings.Join([]string{repositoryBin, externalBin}, string(os.PathListSeparator))}, successfulCheck)
	if err != nil {
		t.Fatal(err)
	}
	assertSameExecutable(t, resolved, external)
}

func TestResolveUsesOutermostRepositoryBoundary(t *testing.T) {
	t.Parallel()

	repository := testRepository(t)
	nested := filepath.Join(repository, "nested")
	if err := os.MkdirAll(filepath.Join(nested, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := writeExecutable(t, filepath.Join(repository, "bin", "trufflehog"))
	if _, err := Resolve("trufflehog", path, nested, nil, successfulCheck); err == nil {
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
	if _, err := Resolve("tool", repositoryLink, repository, nil, successfulCheck); err == nil {
		t.Fatal("Resolve() accepted a repository-local symlink")
	}

	repositoryTool := writeExecutable(t, filepath.Join(repository, "bin", "tool"))
	externalLink := filepath.Join(t.TempDir(), "tool")
	if err := os.Symlink(repositoryTool, externalLink); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve("tool", externalLink, repository, nil, successfulCheck); err == nil {
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
	if _, err := Resolve("git", alias, repository, nil, successfulCheck); err == nil {
		t.Fatalf("Resolve() accepted case alias %q for %q", alias, path)
	}
}

func TestResolveSkipsRelativePATHEntries(t *testing.T) {
	t.Parallel()

	repository := testRepository(t)
	writeExecutable(t, filepath.Join(t.TempDir(), "git"))
	if _, err := Resolve("git", "", repository, []string{"PATH=relative/bin"}, successfulCheck); err == nil {
		t.Fatal("Resolve() accepted a relative PATH entry")
	}
}

func TestResolveRequiresGitWorktreeBoundary(t *testing.T) {
	t.Parallel()

	external := writeExecutable(t, filepath.Join(t.TempDir(), "git"))
	if _, err := Resolve("git", external, t.TempDir(), nil, successfulCheck); err == nil || !strings.Contains(err.Error(), "not inside a Git worktree") {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestResolveSkipsShimThatFailsCapabilityProbe(t *testing.T) {
	t.Parallel()

	repository := testRepository(t)
	shimBin := t.TempDir()
	healthyBin := t.TempDir()
	manager := filepath.Join(shimBin, "manager")
	if err := os.WriteFile(manager, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then exit 0; fi\nexit 9\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(manager, filepath.Join(shimBin, "trufflehog")); err != nil {
		t.Fatal(err)
	}
	healthy := writeExecutable(t, filepath.Join(healthyBin, "trufflehog"))
	environment := []string{
		"PATH=" + strings.Join([]string{shimBin, healthyBin}, string(os.PathListSeparator)),
		"HOME=" + t.TempDir(),
	}
	resolved, err := Resolve("trufflehog", "", repository, environment, Probe([]string{"filesystem"}, t.TempDir(), environment))
	if err != nil {
		t.Fatal(err)
	}
	assertSameExecutable(t, resolved, healthy)
}

func TestResolveStopsWhenProbeCleanupFails(t *testing.T) {
	t.Parallel()

	repository := testRepository(t)
	firstBin := t.TempDir()
	secondBin := t.TempDir()
	writeExecutable(t, filepath.Join(firstBin, "trufflehog"))
	writeExecutable(t, filepath.Join(secondBin, "trufflehog"))
	checks := 0
	check := func(string) error {
		checks++
		if checks == 1 {
			return &probeCleanupError{cause: errors.New("raw cleanup detail")}
		}
		return nil
	}

	path := strings.Join([]string{firstBin, secondBin}, string(os.PathListSeparator))
	_, err := Resolve("trufflehog", "", repository, []string{"PATH=" + path}, check)
	if err == nil || !strings.Contains(err.Error(), "capability probe cleanup failed") {
		t.Fatalf("Resolve() error = %v", err)
	}
	if checks != 1 || strings.Contains(err.Error(), "raw cleanup detail") {
		t.Fatalf("Resolve() checks = %d, error = %v", checks, err)
	}
}

func TestResolvePreservesUsableStandaloneSymlink(t *testing.T) {
	t.Parallel()

	repository := testRepository(t)
	bin := t.TempDir()
	target := writeExecutable(t, filepath.Join(t.TempDir(), "trufflehog-3.90.0"))
	link := filepath.Join(bin, "trufflehog")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	environment := []string{"PATH=" + bin, "HOME=" + t.TempDir()}

	resolved, err := Resolve("trufflehog", "", repository, environment, Probe([]string{"--version"}, t.TempDir(), environment))
	if err != nil {
		t.Fatal(err)
	}
	assertSameExecutable(t, resolved, target)
}

func TestResolveSkipsGitBelowMinimumVersion(t *testing.T) {
	t.Parallel()

	repository := testRepository(t)
	oldBin := t.TempDir()
	newBin := t.TempDir()
	writeExecutableContent(t, filepath.Join(oldBin, "git"), "#!/bin/sh\nprintf 'git version 2.40.9\\n'\n")
	newGit := writeExecutableContent(t, filepath.Join(newBin, "git"), "#!/bin/sh\nprintf 'git version 2.41.0\\n'\n")
	environment := []string{"PATH=" + strings.Join([]string{oldBin, newBin}, string(os.PathListSeparator))}

	resolved, err := Resolve("git", "", repository, environment, GitProbe(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	assertSameExecutable(t, resolved, newGit)
}

func TestGitProbeConcurrentFastProcesses(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	realGit, err = filepath.EvalSymlinks(realGit)
	if err != nil {
		t.Fatal(err)
	}
	check := GitProbe(t.TempDir())
	start := make(chan struct{})
	results := make(chan error, 20)
	var wait sync.WaitGroup
	for range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- check(realGit)
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Errorf("GitProbe() error = %v, cause = %v", err, errors.Unwrap(err))
		}
	}
}

func TestRequireGitVersion(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		output string
		valid  bool
	}{
		{output: "git version 2.41.0", valid: true},
		{output: "git version 2.55.0 (Apple Git-154)", valid: true},
		{output: "git version 3.0.0", valid: true},
		{output: "git version 2.40.9", valid: false},
		{output: "not git", valid: false},
	} {
		err := requireGitVersion(test.output)
		if (err == nil) != test.valid {
			t.Errorf("requireGitVersion(%q) error = %v", test.output, err)
		}
	}
}

func TestResolveDoesNotExposeProbeOutput(t *testing.T) {
	t.Parallel()

	repository := testRepository(t)
	bin := t.TempDir()
	writeExecutableContent(t, filepath.Join(bin, "trufflehog"), "#!/bin/sh\nprintf 'raw dependency output' >&2\nexit 9\n")
	environment := []string{"PATH=" + bin, "HOME=" + t.TempDir()}

	_, err := Resolve("trufflehog", "", repository, environment, Probe([]string{"--version"}, t.TempDir(), environment))
	if err == nil || !strings.Contains(err.Error(), "not usable under the hardened environment") {
		t.Fatalf("Resolve() error = %v", err)
	}
	if strings.Contains(err.Error(), "raw dependency output") {
		t.Fatalf("Resolve() exposed probe output: %v", err)
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
	return writeExecutableContent(t, path, "#!/bin/sh\nexit 0\n")
}

func writeExecutableContent(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func successfulCheck(string) error {
	return nil
}

func assertSameExecutable(t *testing.T, got, want string) {
	t.Helper()
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	wantInfo, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("resolved executable = %q, want %q", got, want)
	}
}
