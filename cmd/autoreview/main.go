package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/uinaf/autoreview/internal/buildinfo"
	"github.com/uinaf/autoreview/internal/config"
	"github.com/uinaf/autoreview/internal/protocol"
	"github.com/uinaf/autoreview/internal/provider"
	"github.com/uinaf/autoreview/internal/target"
)

type dependencies struct {
	lookupEnv    func(string) (string, bool)
	homeDir      func() (string, error)
	newCollector func() (*target.Collector, error)
	newReviewer  func(protocol.ProviderName, string) provider.Reviewer
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, dependencies{
		lookupEnv: os.LookupEnv,
	}))
}

func run(ctx context.Context, arguments []string, stdout, stderr io.Writer, dependencies dependencies) int {
	if len(arguments) == 1 && (arguments[0] == "--version" || arguments[0] == "version") {
		if _, err := fmt.Fprintln(stdout, buildinfo.Version()); err != nil {
			report(stderr, "write version: %v\n", err)
			return 2
		}
		return 0
	}
	if len(arguments) > 0 && arguments[0] == "config" {
		return runConfig(ctx, arguments[1:], stdout, stderr, dependencies)
	}
	if len(arguments) > 0 && arguments[0] == "review" {
		return runReview(ctx, arguments[1:], stdout, stderr, dependencies)
	}
	if len(arguments) == 1 && (arguments[0] == "--help" || arguments[0] == "-h" || arguments[0] == "help") {
		report(stdout, "usage: autoreview <review|config|version> [options]\n\nreview  review a frozen local, branch, or commit target\nconfig  print the effective configuration and its sources\nversion print the binary version\n")
		return 0
	}
	report(stderr, "usage: autoreview <review|config|version> [options]\n")
	return 2
}

func runConfig(ctx context.Context, arguments []string, stdout, stderr io.Writer, dependencies dependencies) int {
	flags := flag.NewFlagSet("autoreview config", flag.ContinueOnError)
	repository := flags.String("repository", ".", "Git repository or path within it")
	configFlags := bindConfigFlags(flags)
	jsonOutput := flags.Bool("json", false, "print effective config as JSON")
	if err := parseFlags(flags, arguments, stdout, stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		report(stderr, "autoreview config does not accept positional arguments\n")
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
		report(stderr, "config: %v\n", err)
		return 2
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(effective); err != nil {
			report(stderr, "encode effective config: %v\n", err)
			return 2
		}
		return 0
	}
	values := []struct {
		name   string
		value  string
		source config.Source
	}{
		{name: "engine", value: string(effective.Engine.Value), source: effective.Engine.Source},
		{name: "model", value: formatQuote(effective.Model.Value), source: effective.Model.Source},
		{name: "reasoning_effort", value: string(effective.ReasoningEffort.Value), source: effective.ReasoningEffort.Source},
		{name: "timeout", value: effective.Timeout.Value.String(), source: effective.Timeout.Source},
		{name: "retries", value: formatInt(effective.Retries.Value), source: effective.Retries.Source},
		{name: "max_bytes", value: formatInt64(effective.MaxBytes.Value), source: effective.MaxBytes.Source},
		{name: "isolation", value: string(effective.Isolation.Value), source: effective.Isolation.Source},
		{name: "web_access", value: formatBool(effective.WebAccess.Value), source: effective.WebAccess.Source},
	}
	for _, value := range values {
		if err := writeConfigValue(stdout, value.name, value.value, value.source); err != nil {
			report(stderr, "write effective config: %v\n", err)
			return 2
		}
	}
	return 0
}

func report(output io.Writer, format string, arguments ...any) {
	_, _ = fmt.Fprintf(output, format, arguments...)
}

func writeConfigValue(output io.Writer, name, value string, source config.Source) error {
	_, err := fmt.Fprintf(output, "%s=%s source=%s\n", name, value, source)
	return err
}
