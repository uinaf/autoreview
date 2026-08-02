package config

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uinaf/autoreview/internal/protocol"
)

func TestLoadUsesDocumentedPrecedenceAndSources(t *testing.T) {
	t.Parallel()

	repository := configRepository(t)
	home := t.TempDir()
	writeConfig(t, filepath.Join(home, ".config", "autoreview", "config.yaml"), "engine: codex\nreasoning_effort: low\ntimeout: 5m\nretries: 0\nisolation: native\nweb_access: true\n")
	writeConfig(t, filepath.Join(repository, ".autoreview.yaml"), "engine: claude\nmodel: repo-model\ntimeout: 6m\nisolation: strict\nweb_access: false\n")
	timeout := 7 * time.Minute
	isolation := protocol.IsolationNative
	web := true
	effective, err := Load(context.Background(), Options{
		Repository: repository,
		LookupEnv: envLookup(map[string]string{
			"AUTOREVIEW_ENGINE":           "cursor",
			"AUTOREVIEW_RETRIES":          "1",
			"AUTOREVIEW_REASONING_EFFORT": "medium",
		}),
		HomeDir:   func() (string, error) { return home, nil },
		Overrides: Overrides{Timeout: &timeout, Isolation: &isolation, WebAccess: &web},
	})
	if err != nil {
		t.Fatal(err)
	}
	if effective.Engine.Value != protocol.ProviderCursor || effective.Engine.Source != SourceEnvironment {
		t.Fatalf("engine = %+v", effective.Engine)
	}
	if effective.Model.Value != "repo-model" || effective.Model.Source != SourceRepository {
		t.Fatalf("model = %+v", effective.Model)
	}
	if effective.ReasoningEffort.Value != ReasoningMedium || effective.ReasoningEffort.Source != SourceEnvironment {
		t.Fatalf("reasoning_effort = %+v", effective.ReasoningEffort)
	}
	if time.Duration(effective.Timeout.Value) != timeout || effective.Timeout.Source != SourceFlag {
		t.Fatalf("timeout = %+v", effective.Timeout)
	}
	if effective.Retries.Value != 1 || effective.Retries.Source != SourceEnvironment {
		t.Fatalf("retries = %+v", effective.Retries)
	}
	if effective.MaxBytes.Value != 1<<20 || effective.MaxBytes.Source != SourceDefault {
		t.Fatalf("max_bytes = %+v", effective.MaxBytes)
	}
	if effective.Isolation.Value != protocol.IsolationNative || effective.Isolation.Source != SourceFlag {
		t.Fatalf("isolation = %+v", effective.Isolation)
	}
	if !effective.WebAccess.Value || effective.WebAccess.Source != SourceFlag {
		t.Fatalf("web_access = %+v", effective.WebAccess)
	}
}

func TestLoadRejectsUnknownRepositoryKeyWithSource(t *testing.T) {
	t.Parallel()

	repository := configRepository(t)
	path := filepath.Join(repository, ".autoreview.yaml")
	writeConfig(t, path, "engine: codex\ncommand: dangerous\n")
	_, err := loadWithoutUserConfig(t, repository, nil)
	if err == nil || !strings.Contains(err.Error(), "repository config") || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "command") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestUntrustedSourcesCannotWeakenIsolation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		repository  string
		environment map[string]string
	}{
		{name: "repository native", repository: "engine: codex\nisolation: native\n"},
		{name: "repository web", repository: "engine: codex\nweb_access: true\n"},
		{name: "environment native", repository: "engine: codex\n", environment: map[string]string{"AUTOREVIEW_ISOLATION": "native"}},
		{name: "environment web", repository: "engine: codex\n", environment: map[string]string{"AUTOREVIEW_WEB_ACCESS": "true"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := configRepository(t)
			writeConfig(t, filepath.Join(repository, ".autoreview.yaml"), test.repository)
			_, err := loadWithoutUserConfig(t, repository, test.environment)
			if err == nil || (!strings.Contains(err.Error(), "cannot enable native") && !strings.Contains(err.Error(), "cannot enable web")) {
				t.Fatalf("Load() error = %v", err)
			}
		})
	}
}

func TestTrustedXDGCanEnableNativeIsolationAndWeb(t *testing.T) {
	t.Parallel()

	repository := configRepository(t)
	home := t.TempDir()
	writeConfig(t, filepath.Join(home, ".config", "autoreview", "config.yaml"), "engine: codex\nisolation: native\nweb_access: true\n")
	effective, err := Load(context.Background(), Options{
		Repository: repository,
		LookupEnv:  envLookup(nil),
		HomeDir:    func() (string, error) { return home, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if effective.Isolation.Value != protocol.IsolationNative || effective.Isolation.Source != SourceXDG || !effective.WebAccess.Value || effective.WebAccess.Source != SourceXDG {
		t.Fatalf("effective controls = isolation %+v, web %+v", effective.Isolation, effective.WebAccess)
	}
}

func TestEnvironmentSelectedXDGCannotEnableCapabilities(t *testing.T) {
	t.Parallel()

	repository := configRepository(t)
	xdg := t.TempDir()
	writeConfig(t, filepath.Join(xdg, "autoreview", "config.yaml"), "engine: codex\nisolation: native\n")
	_, err := Load(context.Background(), Options{
		Repository: repository,
		LookupEnv:  envLookup(map[string]string{"XDG_CONFIG_HOME": xdg}),
		HomeDir:    func() (string, error) { return t.TempDir(), nil },
	})
	if err == nil || !strings.Contains(err.Error(), "xdg config cannot enable native") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestEnvironmentSelectedXDGStillSuppliesNonCapabilityValues(t *testing.T) {
	t.Parallel()

	repository := configRepository(t)
	xdg := t.TempDir()
	writeConfig(t, filepath.Join(xdg, "autoreview", "config.yaml"), "engine: cursor\nmodel: xdg-model\n")
	effective, err := Load(context.Background(), Options{
		Repository: repository,
		LookupEnv:  envLookup(map[string]string{"XDG_CONFIG_HOME": xdg}),
		HomeDir:    func() (string, error) { return t.TempDir(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if effective.Engine.Source != SourceXDG || effective.Model.Value != "xdg-model" || effective.Model.Source != SourceXDG {
		t.Fatalf("effective config = %+v", effective)
	}
}

func TestEnvironmentSelectedXDGInsideRepositorySuppliesNonCapabilityValues(t *testing.T) {
	t.Parallel()

	repository := configRepository(t)
	xdg := filepath.Join(repository, ".xdg")
	writeConfig(t, filepath.Join(xdg, "autoreview", "config.yaml"), "engine: cursor\nmodel: repo-xdg-model\n")
	effective, err := Load(context.Background(), Options{
		Repository: repository,
		LookupEnv:  envLookup(map[string]string{"XDG_CONFIG_HOME": xdg}),
		HomeDir:    func() (string, error) { return t.TempDir(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if effective.Model.Value != "repo-xdg-model" || effective.Model.Source != SourceXDG {
		t.Fatalf("model = %+v", effective.Model)
	}
}

func TestTrustedAccountHomeXDGRejectsParentSymlinkEscape(t *testing.T) {
	t.Parallel()

	repository := configRepository(t)
	home := t.TempDir()
	outside := t.TempDir()
	writeConfig(t, filepath.Join(outside, "autoreview", "config.yaml"), "engine: codex\nisolation: native\n")
	if err := os.Symlink(outside, filepath.Join(home, ".config")); err != nil {
		t.Fatal(err)
	}
	_, err := Load(context.Background(), Options{
		Repository: repository,
		LookupEnv:  envLookup(nil),
		HomeDir:    func() (string, error) { return home, nil },
	})
	if err == nil || !strings.Contains(err.Error(), "xdg config") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestTrustedAccountHomeXDGRejectsOversizedAndWritableFiles(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		content string
		mode    os.FileMode
	}{
		{name: "oversized", content: strings.Repeat(" ", int(maximumConfigBytes)+1), mode: 0o600},
		{name: "group writable", content: "engine: codex\n", mode: 0o620},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := configRepository(t)
			home := t.TempDir()
			path := filepath.Join(home, ".config", "autoreview", "config.yaml")
			writeConfig(t, path, test.content)
			if err := os.Chmod(path, test.mode); err != nil {
				t.Fatal(err)
			}
			_, err := Load(context.Background(), Options{
				Repository: repository,
				LookupEnv:  envLookup(nil),
				HomeDir:    func() (string, error) { return home, nil },
			})
			if err == nil || !strings.Contains(err.Error(), "xdg config") {
				t.Fatalf("Load() error = %v", err)
			}
		})
	}
}

func TestYAMLIntegersUseYAMLGrammar(t *testing.T) {
	t.Parallel()

	repository := configRepository(t)
	writeConfig(t, filepath.Join(repository, ".autoreview.yaml"), "engine: codex\nretries: 0x1\nmax_bytes: 0x100000\n")
	effective, err := loadWithoutUserConfig(t, repository, nil)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Retries.Value != 1 || effective.MaxBytes.Value != 1048576 {
		t.Fatalf("effective integers = retries %d, max_bytes %d", effective.Retries.Value, effective.MaxBytes.Value)
	}
}

func TestYAMLRejectsCoercedScalarsAndNulls(t *testing.T) {
	t.Parallel()

	for _, content := range []string{
		"engine: codex\nmodel: 123\n",
		"engine: codex\nmodel: null\n",
		"engine: codex\nweb_access: \"false\"\n",
		"engine: codex\nretries: \"1\"\n",
	} {
		repository := configRepository(t)
		writeConfig(t, filepath.Join(repository, ".autoreview.yaml"), content)
		if _, err := loadWithoutUserConfig(t, repository, nil); err == nil {
			t.Fatalf("Load() accepted coerced YAML %q", content)
		}
	}
}

func TestInvalidLowerPrecedenceSourceCannotBeHidden(t *testing.T) {
	t.Parallel()

	t.Run("repository", func(t *testing.T) {
		repository := configRepository(t)
		writeConfig(t, filepath.Join(repository, ".autoreview.yaml"), "engine: codex\nretries: 2\n")
		retries := 1
		_, err := Load(context.Background(), Options{
			Repository: repository,
			LookupEnv:  envLookup(map[string]string{"XDG_CONFIG_HOME": t.TempDir()}),
			HomeDir:    func() (string, error) { return t.TempDir(), nil },
			Overrides:  Overrides{Retries: &retries},
		})
		if err == nil || !strings.Contains(err.Error(), "repository retries") {
			t.Fatalf("Load() error = %v", err)
		}
	})
	t.Run("xdg", func(t *testing.T) {
		repository := configRepository(t)
		home := t.TempDir()
		writeConfig(t, filepath.Join(home, ".config", "autoreview", "config.yaml"), "engine: codex\nretries: 2\n")
		writeConfig(t, filepath.Join(repository, ".autoreview.yaml"), "engine: claude\nretries: 1\n")
		_, err := Load(context.Background(), Options{
			Repository: repository,
			LookupEnv:  envLookup(nil),
			HomeDir:    func() (string, error) { return home, nil },
		})
		if err == nil || !strings.Contains(err.Error(), "xdg retries") {
			t.Fatalf("Load() error = %v", err)
		}
	})
	t.Run("environment", func(t *testing.T) {
		repository := configRepository(t)
		writeConfig(t, filepath.Join(repository, ".autoreview.yaml"), "engine: codex\n")
		reasoning := ReasoningHigh
		_, err := Load(context.Background(), Options{
			Repository: repository,
			LookupEnv: envLookup(map[string]string{
				"XDG_CONFIG_HOME":             t.TempDir(),
				"AUTOREVIEW_REASONING_EFFORT": "impossible",
			}),
			HomeDir:   func() (string, error) { return t.TempDir(), nil },
			Overrides: Overrides{ReasoningEffort: &reasoning},
		})
		if err == nil || !strings.Contains(err.Error(), "environment reasoning_effort") {
			t.Fatalf("Load() error = %v", err)
		}
	})
}

func TestLoadRequiresExplicitEngineAndIgnoresPATHCandidates(t *testing.T) {
	t.Parallel()

	repository := configRepository(t)
	_, err := loadWithoutUserConfig(t, repository, map[string]string{"PATH": "/contains/codex"})
	if err == nil || !strings.Contains(err.Error(), "engine is required") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadUsesOnlyRootRepositoryConfig(t *testing.T) {
	t.Parallel()

	repository := configRepository(t)
	writeConfig(t, filepath.Join(repository, ".autoreview.yaml"), "engine: claude\n")
	nested := filepath.Join(repository, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, filepath.Join(nested, ".autoreview.yaml"), "engine: cursor\n")
	effective, err := loadWithoutUserConfig(t, nested, nil)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Engine.Value != protocol.ProviderClaude || effective.Engine.Source != SourceRepository {
		t.Fatalf("engine = %+v", effective.Engine)
	}
}

func TestLoadRejectsRepositoryConfigSymlink(t *testing.T) {
	t.Parallel()

	repository := configRepository(t)
	target := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, target, "engine: codex\n")
	if err := os.Symlink(target, filepath.Join(repository, ".autoreview.yaml")); err != nil {
		t.Fatal(err)
	}
	_, err := loadWithoutUserConfig(t, repository, nil)
	if err == nil || !strings.Contains(err.Error(), "repository config") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsMultipleDocumentsAndRetryOverflow(t *testing.T) {
	t.Parallel()

	for _, content := range []string{
		"engine: codex\n---\nengine: claude\n",
		"engine: codex\nretries: 2\n",
	} {
		repository := configRepository(t)
		writeConfig(t, filepath.Join(repository, ".autoreview.yaml"), content)
		if _, err := loadWithoutUserConfig(t, repository, nil); err == nil {
			t.Fatalf("Load() accepted %q", content)
		}
	}
}

func TestEffectiveDiagnosticUsesDurationStringAndNoEnvironmentDump(t *testing.T) {
	t.Parallel()

	effective := defaults()
	effective.Engine = Value[protocol.ProviderName]{Value: protocol.ProviderCodex, Source: SourceFlag}
	encoded, err := json.Marshal(effective)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"timeout":{"value":"15m0s","source":"default"}`) || strings.Contains(text, "API_KEY") {
		t.Fatalf("diagnostic JSON = %s", text)
	}
}

func TestPrepareStrictRuntimeSanitizesStateAndUsesEmptyWorkspace(t *testing.T) {
	t.Parallel()

	effective := defaults()
	effective.Engine.Value = protocol.ProviderCodex
	runtime, err := PrepareRuntime(effective, []string{
		"PATH=/usr/bin",
		"HOME=/private/home",
		"CODEX_HOME=/private/codex",
		"OPENAI_API_KEY=secret",
		"CODEX_API_KEY=codex-secret",
		"ANTHROPIC_API_KEY=claude-secret",
		"CURSOR_API_KEY=cursor-secret",
		"ALL_PROXY=socks5://proxy.example:1080",
		"NODE_EXTRA_CA_CERTS=/etc/company-ca.pem",
		"no_proxy=localhost,127.0.0.1",
		"AWS_SECRET_ACCESS_KEY=drop-me",
	})
	if err != nil {
		t.Fatal(err)
	}
	root := runtime.root
	defer func() { _ = runtime.Close() }()
	entries, err := os.ReadDir(runtime.Workspace)
	if err != nil || len(entries) != 0 {
		t.Fatalf("workspace entries = %v, error = %v", entries, err)
	}
	environment := strings.Join(runtime.Environment(), "\n")
	for _, expected := range []string{
		"PATH=/usr/bin",
		"OPENAI_API_KEY=secret",
		"CODEX_API_KEY=codex-secret",
		"ALL_PROXY=socks5://proxy.example:1080",
		"NODE_EXTRA_CA_CERTS=/etc/company-ca.pem",
		"no_proxy=localhost,127.0.0.1",
		"HOME=" + filepath.Join(root, "home"),
		"CODEX_HOME=" + filepath.Join(root, "codex"),
	} {
		if !strings.Contains(environment, expected) {
			t.Errorf("strict environment omitted %q: %s", expected, environment)
		}
	}
	for _, forbidden := range []string{"HOME=/private/home", "CODEX_HOME=/private/codex", "ANTHROPIC_API_KEY", "CURSOR_API_KEY", "AWS_SECRET_ACCESS_KEY"} {
		if strings.Contains(environment, forbidden) {
			t.Errorf("strict environment retained %q: %s", forbidden, environment)
		}
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime root still exists: %v", err)
	}
}

func TestPrepareNativeRuntimePreservesEnvironmentWithEmptyWorkspace(t *testing.T) {
	t.Parallel()

	effective := defaults()
	effective.Engine.Value = protocol.ProviderClaude
	effective.Isolation.Value = protocol.IsolationNative
	parent := []string{"HOME=/native/home", "CLAUDE_CONFIG_DIR=/native/claude", "TOKEN=secret"}
	runtime, err := PrepareRuntime(effective, parent)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Close() }()
	if !reflect.DeepEqual(runtime.Environment(), parent) {
		t.Fatalf("environment = %v, want %v", runtime.Environment(), parent)
	}
	entries, err := os.ReadDir(runtime.Workspace)
	if err != nil || len(entries) != 0 {
		t.Fatalf("workspace entries = %v, error = %v", entries, err)
	}
}

func TestRuntimeCloseCanRetryAfterFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	attempts := 0
	runtime := &Runtime{
		root: root,
		removeAll: func(path string) error {
			attempts++
			if attempts == 1 {
				return errors.New("injected cleanup failure")
			}
			return os.RemoveAll(path)
		},
	}
	if err := runtime.Close(); err == nil || !strings.Contains(err.Error(), "remove provider runtime") {
		t.Fatalf("first Close() error = %v", err)
	}
	if runtime.root != root {
		t.Fatalf("runtime root cleared after failed cleanup: %q", runtime.root)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("retry Close(): %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime root still exists: %v", err)
	}
	if err := runtime.Close(); err != nil || attempts != 2 {
		t.Fatalf("idempotent Close() error = %v, attempts = %d", err, attempts)
	}
}

func TestRuntimeCloseIsConcurrentAndIdempotent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	attempts := 0
	runtime := &Runtime{
		root: root,
		removeAll: func(path string) error {
			attempts++
			return os.RemoveAll(path)
		},
	}
	const callers = 8
	errors := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errors <- runtime.Close()
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}
	if attempts != 1 {
		t.Fatalf("cleanup attempts = %d, want 1", attempts)
	}
}

func loadWithoutUserConfig(t *testing.T, repository string, environment map[string]string) (Effective, error) {
	t.Helper()
	values := map[string]string{"XDG_CONFIG_HOME": t.TempDir()}
	for name, value := range environment {
		values[name] = value
	}
	return Load(context.Background(), Options{
		Repository: repository,
		LookupEnv:  envLookup(values),
		HomeDir:    func() (string, error) { return t.TempDir(), nil },
	})
}

func envLookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func configRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	command := exec.CommandContext(t.Context(), "git", "init", "-q", "-b", "main")
	command.Dir = repository
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	return repository
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
