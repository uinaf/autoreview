package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/uinaf/autoreview/internal/config"
	"github.com/uinaf/autoreview/internal/orchestrator"
	"github.com/uinaf/autoreview/internal/protocol"
	"github.com/uinaf/autoreview/internal/provider"
	reportwriter "github.com/uinaf/autoreview/internal/report"
	"github.com/uinaf/autoreview/internal/target"
)

type stringList []string

func (values *stringList) String() string { return fmt.Sprint([]string(*values)) }

func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func runReview(ctx context.Context, arguments []string, stdout, stderr io.Writer, dependencies dependencies) int {
	flags := flag.NewFlagSet("autoreview review", flag.ContinueOnError)
	repository := flags.String("repository", ".", "Git repository or path within it")
	mode := flags.String("mode", "", "target mode: local, branch, or commit")
	base := flags.String("base", "", "base revision for branch mode")
	commit := flags.String("commit", "", "revision for commit mode")
	prompt := flags.String("prompt", "", "trusted review instructions")
	var contextFiles stringList
	flags.Var(&contextFiles, "context-file", "repository-relative context file (repeatable)")
	output := flags.String("output", "terminal", "output format: terminal or json")
	configFlags := bindConfigFlags(flags)
	if err := parseFlags(flags, arguments, stdout, stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		report(stderr, "autoreview review does not accept positional arguments\n")
		return 2
	}
	if *output != "terminal" && *output != "json" {
		report(stderr, "flag output must be terminal or json\n")
		return 2
	}
	overrides, err := configFlags.overrides(flags)
	if err != nil {
		report(stderr, "%v\n", err)
		return 2
	}
	effective, err := config.Load(ctx, config.Options{
		Repository: *repository,
		Overrides:  overrides,
		LookupEnv:  dependencies.lookupEnv,
		HomeDir:    dependencies.homeDir,
	})
	if err != nil {
		return writeReviewResult(stdout, stderr, *output, orchestrator.Failure(protocol.FailureConfig, fmt.Errorf("config: %w", err)))
	}

	newCollector := dependencies.newCollector
	if newCollector == nil {
		newCollector = func() (*target.Collector, error) { return target.New(target.Options{}) }
	}
	collector, err := newCollector()
	if err != nil {
		return writeReviewResult(stdout, stderr, *output, orchestrator.Failure(protocol.FailureCapability, fmt.Errorf("initialize target collector: %w", err)))
	}
	newReviewer := dependencies.newReviewer
	if newReviewer == nil {
		newReviewer = defaultReviewer
	}
	result := orchestrator.Run(ctx, orchestrator.Options{
		Collector:   collector,
		NewReviewer: newReviewer,
		Repository:  *repository,
		Target: target.Request{
			Mode:         protocol.TargetMode(*mode),
			Base:         *base,
			Commit:       *commit,
			Prompt:       *prompt,
			ContextFiles: append([]string(nil), contextFiles...),
			MaxBytes:     effective.MaxBytes.Value,
		},
		Config: effective,
		Progress: func(message string) {
			report(stderr, "%s\n", message)
		},
	})
	return writeReviewResult(stdout, stderr, *output, result)
}

func defaultReviewer(name protocol.ProviderName, repository string) provider.Reviewer {
	switch name {
	case protocol.ProviderCodex:
		return provider.NewCodex(provider.CodexOptions{Repository: repository})
	case protocol.ProviderClaude:
		return provider.NewClaude(provider.ClaudeOptions{Repository: repository})
	case protocol.ProviderCursor:
		return provider.NewCursor(provider.CursorOptions{Repository: repository})
	default:
		return nil
	}
}

func writeReviewResult(stdout, stderr io.Writer, output string, result protocol.Report) int {
	var err error
	if output == "json" {
		err = reportwriter.WriteJSON(stdout, result)
	} else {
		err = reportwriter.WriteTerminal(stdout, result)
	}
	if err != nil {
		report(stderr, "write review result: %v\n", err)
		return 2
	}
	switch result.Status {
	case protocol.StatusClean:
		return 0
	case protocol.StatusFindings:
		return 1
	default:
		return 2
	}
}
