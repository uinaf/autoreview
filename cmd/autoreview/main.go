package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/uinaf/autoreview/internal/buildinfo"
	"github.com/uinaf/autoreview/internal/config"
	"github.com/uinaf/autoreview/internal/protocol"
)

type dependencies struct {
	lookupEnv func(string) (string, bool)
	homeDir   func() (string, error)
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, dependencies{
		lookupEnv: os.LookupEnv,
	}))
}

func run(ctx context.Context, arguments []string, stdout, stderr io.Writer, dependencies dependencies) int {
	if len(arguments) == 1 && (arguments[0] == "--version" || arguments[0] == "version") {
		if _, err := fmt.Fprintln(stdout, buildinfo.Version()); err != nil {
			fmt.Fprintf(stderr, "write version: %v\n", err)
			return 2
		}
		return 0
	}
	if len(arguments) > 0 && arguments[0] == "config" {
		return runConfig(ctx, arguments[1:], stdout, stderr, dependencies)
	}
	fmt.Fprintln(stderr, "usage: autoreview --version | autoreview config [options]")
	return 2
}

func runConfig(ctx context.Context, arguments []string, stdout, stderr io.Writer, dependencies dependencies) int {
	flags := flag.NewFlagSet("autoreview config", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repository", ".", "Git repository or path within it")
	engine := flags.String("engine", "", "review engine: codex, claude, or cursor")
	model := flags.String("model", "", "provider model override")
	reasoning := flags.String("reasoning-effort", "", "reasoning effort")
	timeoutText := flags.String("timeout", "", "provider timeout")
	retries := flags.Int("retries", 0, "protocol retry count: 0 or 1")
	maxBytes := flags.Int64("max-bytes", 0, "maximum frozen bundle bytes")
	isolation := flags.String("isolation", "", "provider isolation: strict or native")
	webAccess := flags.Bool("web-access", false, "allow provider web access")
	jsonOutput := flags.Bool("json", false, "print effective config as JSON")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "autoreview config does not accept positional arguments")
		return 2
	}
	visited := map[string]bool{}
	flags.Visit(func(item *flag.Flag) { visited[item.Name] = true })
	overrides := config.Overrides{}
	if visited["engine"] {
		value := protocol.ProviderName(*engine)
		overrides.Engine = &value
	}
	if visited["model"] {
		overrides.Model = model
	}
	if visited["reasoning-effort"] {
		value := config.ReasoningEffort(*reasoning)
		overrides.ReasoningEffort = &value
	}
	if visited["timeout"] {
		value, err := time.ParseDuration(*timeoutText)
		if err != nil {
			fmt.Fprintf(stderr, "flag timeout: %v\n", err)
			return 2
		}
		overrides.Timeout = &value
	}
	if visited["retries"] {
		overrides.Retries = retries
	}
	if visited["max-bytes"] {
		overrides.MaxBytes = maxBytes
	}
	if visited["isolation"] {
		value := protocol.Isolation(*isolation)
		overrides.Isolation = &value
	}
	if visited["web-access"] {
		overrides.WebAccess = webAccess
	}
	effective, err := config.Load(ctx, config.Options{
		Repository: *repository,
		Overrides:  overrides,
		LookupEnv:  dependencies.lookupEnv,
		HomeDir:    dependencies.homeDir,
	})
	if err != nil {
		fmt.Fprintf(stderr, "config: %v\n", err)
		return 2
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(effective); err != nil {
			fmt.Fprintf(stderr, "encode effective config: %v\n", err)
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
		{name: "model", value: strconv.Quote(effective.Model.Value), source: effective.Model.Source},
		{name: "reasoning_effort", value: string(effective.ReasoningEffort.Value), source: effective.ReasoningEffort.Source},
		{name: "timeout", value: effective.Timeout.Value.String(), source: effective.Timeout.Source},
		{name: "retries", value: strconv.Itoa(effective.Retries.Value), source: effective.Retries.Source},
		{name: "max_bytes", value: strconv.FormatInt(effective.MaxBytes.Value, 10), source: effective.MaxBytes.Source},
		{name: "isolation", value: string(effective.Isolation.Value), source: effective.Isolation.Source},
		{name: "web_access", value: strconv.FormatBool(effective.WebAccess.Value), source: effective.WebAccess.Source},
	}
	for _, value := range values {
		if err := writeConfigValue(stdout, value.name, value.value, value.source); err != nil {
			fmt.Fprintf(stderr, "write effective config: %v\n", err)
			return 2
		}
	}
	return 0
}

func writeConfigValue(output io.Writer, name, value string, source config.Source) error {
	_, err := fmt.Fprintf(output, "%s=%s source=%s\n", name, value, source)
	return err
}
