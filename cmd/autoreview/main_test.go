package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uinaf/autoreview/internal/config"
	"github.com/uinaf/autoreview/internal/protocol"
	"github.com/uinaf/autoreview/internal/provider"
	"github.com/uinaf/autoreview/internal/target"
)

func TestReviewCommandCleanJSON(t *testing.T) {
	t.Parallel()

	repository := reviewRepository(t)
	reviewer := &scriptedReviewer{results: []reviewStep{{result: cleanResult()}}}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := run(t.Context(), []string{
		"review", "--repository", repository, "--mode", "local", "--engine", "codex",
		"--prompt", "Check the acceptance criteria.", "--context-file", "CONTEXT.md", "--output", "json",
	}, &stdout, &stderr, reviewDependencies(t, cleanScanner{}, reviewer))
	if exit != 0 {
		t.Fatalf("run() exit = %d, stderr = %s, stdout = %s", exit, stderr.String(), stdout.String())
	}
	var result protocol.Report
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode report: %v: %s", err, stdout.String())
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("report validation: %v", err)
	}
	if result.Status != protocol.StatusClean || len(result.Metadata.Attempts) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(reviewer.prompts[0], "Check the acceptance criteria.") || !strings.Contains(reviewer.prompts[0], "context evidence") {
		t.Fatalf("provider prompt omitted trusted prompt or context: %s", reviewer.prompts[0])
	}
	if strings.Contains(stdout.String(), "collecting frozen target") || !strings.Contains(stderr.String(), "reviewing with codex") {
		t.Fatalf("stdout/stderr separation failed: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestReviewCommandPrintsEveryFindingAndExitsOne(t *testing.T) {
	t.Parallel()

	repository := reviewRepository(t)
	result := cleanResult()
	result.Review.Findings = []protocol.Finding{
		finding("high confidence", 0.99),
		finding("low confidence", 0.01),
	}
	reviewer := &scriptedReviewer{results: []reviewStep{{result: result}}}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := run(t.Context(), []string{"review", "--repository", repository, "--mode", "local", "--engine", "codex"}, &stdout, &stderr, reviewDependencies(t, cleanScanner{}, reviewer))
	if exit != 1 || !strings.Contains(stdout.String(), "high confidence") || !strings.Contains(stdout.String(), "low confidence") {
		t.Fatalf("run() exit = %d, stdout = %s, stderr = %s", exit, stdout.String(), stderr.String())
	}
}

func TestReviewCommandRetriesOnlyProtocolFailures(t *testing.T) {
	t.Parallel()

	repository := reviewRepository(t)
	reviewer := &scriptedReviewer{results: []reviewStep{
		{err: providerError(protocol.FailureProtocol, protocol.AttemptMalformed)},
		{result: cleanResult()},
	}}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := run(t.Context(), []string{"review", "--repository", repository, "--mode", "local", "--engine", "codex", "--retries", "1", "--output", "json"}, &stdout, &stderr, reviewDependencies(t, cleanScanner{}, reviewer))
	if exit != 0 {
		t.Fatalf("run() exit = %d, stderr = %s, stdout = %s", exit, stderr.String(), stdout.String())
	}
	var result protocol.Report
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(reviewer.prompts) != 2 || len(result.Metadata.Attempts) != 2 || result.Metadata.Attempts[0].Outcome != protocol.AttemptMalformed {
		t.Fatalf("prompts=%d attempts=%+v", len(reviewer.prompts), result.Metadata.Attempts)
	}
	if !strings.HasPrefix(reviewer.prompts[1], reviewer.prompts[0]) || !strings.Contains(stderr.String(), "retrying malformed provider response") {
		t.Fatalf("retry did not reuse frozen prompt: stderr=%q", stderr.String())
	}

	authReviewer := &scriptedReviewer{results: []reviewStep{{err: providerError(protocol.FailureAuth, protocol.AttemptFailed)}, {result: cleanResult()}}}
	stdout.Reset()
	stderr.Reset()
	exit = run(t.Context(), []string{"review", "--repository", repository, "--mode", "local", "--engine", "codex", "--retries", "1", "--output", "json"}, &stdout, &stderr, reviewDependencies(t, cleanScanner{}, authReviewer))
	if exit != 2 || len(authReviewer.prompts) != 1 {
		t.Fatalf("authentication failure retried: exit=%d prompts=%d output=%s", exit, len(authReviewer.prompts), stdout.String())
	}
}

func TestReviewCommandClassifiesPlainReviewerError(t *testing.T) {
	t.Parallel()

	repository := reviewRepository(t)
	reviewer := &scriptedReviewer{results: []reviewStep{{err: errors.New("unwrapped reviewer failure")}, {result: cleanResult()}}}
	var stdout bytes.Buffer
	exit := run(t.Context(), []string{"review", "--repository", repository, "--mode", "local", "--engine", "codex", "--retries", "1", "--output", "json"}, &stdout, io.Discard, reviewDependencies(t, cleanScanner{}, reviewer))
	if exit != 2 || len(reviewer.prompts) != 1 {
		t.Fatalf("exit=%d calls=%d output=%s", exit, len(reviewer.prompts), stdout.String())
	}
	var result protocol.Report
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Failure == nil || result.Failure.Class != protocol.FailureProvider || len(result.Metadata.Attempts) != 1 || result.Metadata.Attempts[0].Outcome != protocol.AttemptFailed {
		t.Fatalf("result=%+v", result)
	}
}

func TestReviewCommandRejectsInvalidFindingBoundaries(t *testing.T) {
	t.Parallel()

	repository := reviewRepository(t)
	invalid := cleanResult()
	invalid.Review.Findings = []protocol.Finding{finding("outside target", 0.5)}
	invalid.Review.Findings[0].Location.FilePath = "other.go"
	reviewer := &scriptedReviewer{results: []reviewStep{{result: invalid}, {result: invalid}}}
	var stdout bytes.Buffer
	exit := run(t.Context(), []string{"review", "--repository", repository, "--mode", "local", "--engine", "codex", "--retries", "1", "--output", "json"}, &stdout, io.Discard, reviewDependencies(t, cleanScanner{}, reviewer))
	if exit != 2 || len(reviewer.prompts) != 2 {
		t.Fatalf("run() exit = %d, prompts = %d, output = %s", exit, len(reviewer.prompts), stdout.String())
	}
	var result protocol.Report
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Failure == nil || result.Failure.Class != protocol.FailureProtocol || len(result.Metadata.Attempts) != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestReviewCommandClassifiesSecretTimeoutAndCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		scanner     target.Scanner
		reviewer    *scriptedReviewer
		wantClass   protocol.FailureClass
		wantMessage string
		wantCalls   int
	}{
		{name: "secret", scanner: scannerError{err: target.ErrSecretFound}, reviewer: &scriptedReviewer{}, wantClass: protocol.FailureSecretScan, wantMessage: target.ErrSecretFound.Error(), wantCalls: 0},
		{name: "timeout", scanner: cleanScanner{}, reviewer: &scriptedReviewer{results: []reviewStep{{err: providerError(protocol.FailureTimeout, protocol.AttemptFailed)}}}, wantClass: protocol.FailureTimeout, wantCalls: 1},
		{name: "cancelled", scanner: cleanScanner{}, reviewer: &scriptedReviewer{results: []reviewStep{{err: providerError(protocol.FailureCancelled, protocol.AttemptFailed)}}}, wantClass: protocol.FailureCancelled, wantCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := reviewRepository(t)
			var stdout bytes.Buffer
			exit := run(t.Context(), []string{"review", "--repository", repository, "--mode", "local", "--engine", "codex", "--output", "json"}, &stdout, io.Discard, reviewDependencies(t, test.scanner, test.reviewer))
			if exit != 2 || len(test.reviewer.prompts) != test.wantCalls {
				t.Fatalf("exit=%d calls=%d output=%s", exit, len(test.reviewer.prompts), stdout.String())
			}
			var result protocol.Report
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Failure == nil || result.Failure.Class != test.wantClass {
				t.Fatalf("failure = %+v", result.Failure)
			}
			if test.wantMessage != "" && result.Failure.Message != test.wantMessage {
				t.Fatalf("failure message = %q, want %q", result.Failure.Message, test.wantMessage)
			}
		})
	}
}

func TestReviewCommandRefusesSourceMutation(t *testing.T) {
	t.Parallel()

	repository := reviewRepository(t)
	reviewer := &scriptedReviewer{results: []reviewStep{{before: func() {
		if err := os.WriteFile(filepath.Join(repository, "app.go"), []byte("mutated\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}, result: cleanResult()}}}
	var stdout bytes.Buffer
	exit := run(t.Context(), []string{"review", "--repository", repository, "--mode", "local", "--engine", "codex", "--output", "json"}, &stdout, io.Discard, reviewDependencies(t, cleanScanner{}, reviewer))
	if exit != 2 {
		t.Fatalf("run() exit = %d, output = %s", exit, stdout.String())
	}
	var result protocol.Report
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Failure == nil || result.Failure.Class != protocol.FailureSourceChanged {
		t.Fatalf("failure = %+v", result.Failure)
	}
	if len(result.Metadata.Attempts) != 1 || result.Metadata.Attempts[0].Outcome != protocol.AttemptValid {
		t.Fatalf("completed provider attempt was lost: %+v", result.Metadata.Attempts)
	}
}

func TestReviewCommandReportsOutputFailure(t *testing.T) {
	t.Parallel()

	repository := reviewRepository(t)
	var stderr bytes.Buffer
	exit := run(t.Context(), []string{"review", "--repository", repository, "--mode", "local", "--engine", "codex"}, failingWriter{}, &stderr, reviewDependencies(t, cleanScanner{}, &scriptedReviewer{results: []reviewStep{{result: cleanResult()}}}))
	if exit != 2 || !strings.Contains(stderr.String(), "write review result") {
		t.Fatalf("run() exit = %d, stderr = %s", exit, stderr.String())
	}
}

func TestTopLevelAndReviewHelp(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	if exit := run(t.Context(), []string{"--help"}, &stdout, io.Discard, dependencies{}); exit != 0 || !strings.Contains(stdout.String(), "autoreview <review|config|version>") {
		t.Fatalf("top-level help exit=%d output=%q", exit, stdout.String())
	}
	stdout.Reset()
	if exit := run(t.Context(), []string{"review", "--help"}, &stdout, io.Discard, dependencies{}); exit != 0 || !strings.Contains(stdout.String(), "Usage of autoreview review") {
		t.Fatalf("review help exit=%d output=%q", exit, stdout.String())
	}
}

func TestInvalidFlagBeforeHelpStaysOnStderr(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := run(t.Context(), []string{"review", "--unknown", "--help"}, &stdout, &stderr, dependencies{})
	if exit != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

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

func TestConfigHelpIsSuccessful(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	exit := run(context.Background(), []string{"config", "--help"}, &stdout, io.Discard, dependencies{})
	if exit != 0 || !strings.Contains(stdout.String(), "Usage of autoreview config") {
		t.Fatalf("run() exit = %d, stdout = %s", exit, stdout.String())
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

type reviewStep struct {
	before func()
	result provider.Result
	err    error
}

type scriptedReviewer struct {
	results []reviewStep
	prompts []string
}

func (reviewer *scriptedReviewer) Review(_ context.Context, request provider.Request) (provider.Result, error) {
	reviewer.prompts = append(reviewer.prompts, request.Prompt)
	index := len(reviewer.prompts) - 1
	if index >= len(reviewer.results) {
		return provider.Result{}, errors.New("unexpected review call")
	}
	step := reviewer.results[index]
	if step.before != nil {
		step.before()
	}
	return step.result, step.err
}

type cleanScanner struct{}

func (cleanScanner) Scan(context.Context, []byte) error { return nil }

type scannerError struct{ err error }

func (scanner scannerError) Scan(context.Context, []byte) error { return scanner.err }

func cleanResult() provider.Result {
	return provider.Result{
		Review: protocol.Review{
			Findings:           []protocol.Finding{},
			OverallExplanation: "No actionable defects found.",
			OverallConfidence:  0.9,
		},
		Provider: protocol.Provider{Name: protocol.ProviderCodex, Model: "fake-model", Version: "1.2.3"},
		Attempt: protocol.Attempt{
			Number:     1,
			Outcome:    protocol.AttemptValid,
			DurationMS: 1,
		},
		Duration:  time.Millisecond,
		Isolation: protocol.IsolationStrict,
	}
}

func finding(title string, confidence float64) protocol.Finding {
	return protocol.Finding{
		Title:      title,
		Body:       "This behavior is incorrect.",
		Priority:   protocol.PriorityP1,
		Confidence: confidence,
		Category:   protocol.CategoryBug,
		Location:   protocol.Location{FilePath: "app.go", StartLine: 1, EndLine: 1},
	}
}

func providerError(class protocol.FailureClass, outcome protocol.AttemptOutcome) error {
	return &provider.Error{
		Class:   class,
		Message: "injected provider failure",
		Attempt: &protocol.Attempt{Number: 1, Outcome: outcome, DurationMS: 1, ErrorClass: &class},
	}
}

func reviewDependencies(t *testing.T, scanner target.Scanner, reviewer provider.Reviewer) dependencies {
	t.Helper()
	xdg := t.TempDir()
	return dependencies{
		lookupEnv: func(name string) (string, bool) {
			if name == "XDG_CONFIG_HOME" {
				return xdg, true
			}
			return "", false
		},
		homeDir: func() (string, error) { return t.TempDir(), nil },
		newCollector: func() (*target.Collector, error) {
			return target.New(target.Options{Scanner: scanner})
		},
		newReviewer: func(protocol.ProviderName, string) provider.Reviewer { return reviewer },
	}
}

func reviewRepository(t *testing.T) string {
	t.Helper()
	repository := cliRepository(t)
	if err := os.WriteFile(filepath.Join(repository, "app.go"), []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "CONTEXT.md"), []byte("context evidence\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, repository, "add", "app.go", "CONTEXT.md")
	gitCommand(t, repository, "-c", "user.name=Autoreview Test", "-c", "user.email=test@example.invalid", "commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(repository, "app.go"), []byte("new application content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return repository
}

func gitCommand(t *testing.T, repository string, arguments ...string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", arguments...)
	command.Dir = repository
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
}
