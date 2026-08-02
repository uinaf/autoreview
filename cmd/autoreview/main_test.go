package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uinaf/autoreview/internal/config"
)

func TestConfigCommandPrintsSourceAwareJSON(t *testing.T) {
	t.Parallel()

	repository := cliRepository(t)
	xdg := t.TempDir()
	lookup := func(name string) (string, bool) {
		if name == "XDG_CONFIG_HOME" {
			return xdg, true
		}
		return "", false
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := run(context.Background(), []string{"config", "--repository", repository, "--engine", "codex", "--web-access", "--json"}, &stdout, &stderr, dependencies{
		lookupEnv: lookup,
		homeDir:   func() (string, error) { return t.TempDir(), nil },
	})
	if exit != 0 {
		t.Fatalf("run() exit = %d, stderr = %s", exit, stderr.String())
	}
	var effective config.Effective
	if err := json.Unmarshal(stdout.Bytes(), &effective); err != nil {
		t.Fatalf("decode JSON: %v: %s", err, stdout.String())
	}
	if effective.Engine.Source != config.SourceFlag || effective.WebAccess.Source != config.SourceFlag || !effective.WebAccess.Value {
		t.Fatalf("effective config = %+v", effective)
	}
	if strings.Contains(stdout.String(), "XDG_CONFIG_HOME") {
		t.Fatalf("diagnostic leaked environment details: %s", stdout.String())
	}
}

func TestConfigCommandRejectsMissingEngine(t *testing.T) {
	t.Parallel()

	repository := cliRepository(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := run(context.Background(), []string{"config", "--repository", repository}, &stdout, &stderr, dependencies{
		lookupEnv: func(name string) (string, bool) {
			if name == "XDG_CONFIG_HOME" {
				return t.TempDir(), true
			}
			return "", false
		},
		homeDir: func() (string, error) { return t.TempDir(), nil },
	})
	if exit != 2 || !strings.Contains(stderr.String(), "engine is required") {
		t.Fatalf("run() exit = %d, stderr = %s", exit, stderr.String())
	}
}

func TestConfigCommandReportsPlainOutputFailure(t *testing.T) {
	t.Parallel()

	repository := cliRepository(t)
	var stderr bytes.Buffer
	exit := run(context.Background(), []string{"config", "--repository", repository, "--engine", "codex"}, failingWriter{}, &stderr, dependencies{
		lookupEnv: func(name string) (string, bool) {
			if name == "XDG_CONFIG_HOME" {
				return t.TempDir(), true
			}
			return "", false
		},
		homeDir: func() (string, error) { return t.TempDir(), nil },
	})
	if exit != 2 || !strings.Contains(stderr.String(), "write effective config") {
		t.Fatalf("run() exit = %d, stderr = %s", exit, stderr.String())
	}
}

func TestVersionCommandReportsOutputFailure(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	exit := run(context.Background(), []string{"--version"}, failingWriter{}, &stderr, dependencies{})
	if exit != 2 || !strings.Contains(stderr.String(), "write version") {
		t.Fatalf("run() exit = %d, stderr = %s", exit, stderr.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("injected write failure")
}

func cliRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	command := exec.CommandContext(t.Context(), "git", "init", "-q", "-b", "main")
	command.Dir = repository
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err := os.MkdirAll(filepath.Join(repository, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	return repository
}
