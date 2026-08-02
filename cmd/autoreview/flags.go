package main

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/uinaf/autoreview/internal/config"
	"github.com/uinaf/autoreview/internal/protocol"
)

func flagOutput(arguments []string, stdout, stderr io.Writer) io.Writer {
	for _, argument := range arguments {
		if argument == "-h" || argument == "--help" {
			return stdout
		}
	}
	return stderr
}

type configFlagValues struct {
	engine    *string
	model     *string
	reasoning *string
	timeout   *string
	retries   *int
	maxBytes  *int64
	isolation *string
	webAccess *bool
}

func bindConfigFlags(flags *flag.FlagSet) configFlagValues {
	return configFlagValues{
		engine:    flags.String("engine", "", "review engine: codex, claude, or cursor"),
		model:     flags.String("model", "", "provider model override"),
		reasoning: flags.String("reasoning-effort", "", "reasoning effort"),
		timeout:   flags.String("timeout", "", "provider timeout"),
		retries:   flags.Int("retries", 0, "protocol retry count: 0 or 1"),
		maxBytes:  flags.Int64("max-bytes", 0, "maximum frozen bundle bytes"),
		isolation: flags.String("isolation", "", "provider isolation: strict or native"),
		webAccess: flags.Bool("web-access", false, "allow provider web access"),
	}
}

func (values configFlagValues) overrides(flags *flag.FlagSet) (config.Overrides, error) {
	visited := map[string]bool{}
	flags.Visit(func(item *flag.Flag) { visited[item.Name] = true })
	overrides := config.Overrides{}
	if visited["engine"] {
		value := protocol.ProviderName(*values.engine)
		overrides.Engine = &value
	}
	if visited["model"] {
		overrides.Model = values.model
	}
	if visited["reasoning-effort"] {
		value := config.ReasoningEffort(*values.reasoning)
		overrides.ReasoningEffort = &value
	}
	if visited["timeout"] {
		value, err := time.ParseDuration(*values.timeout)
		if err != nil {
			return config.Overrides{}, fmt.Errorf("flag timeout: %w", err)
		}
		overrides.Timeout = &value
	}
	if visited["retries"] {
		overrides.Retries = values.retries
	}
	if visited["max-bytes"] {
		overrides.MaxBytes = values.maxBytes
	}
	if visited["isolation"] {
		value := protocol.Isolation(*values.isolation)
		overrides.Isolation = &value
	}
	if visited["web-access"] {
		overrides.WebAccess = values.webAccess
	}
	return overrides, nil
}

func formatInt(value int) string      { return strconv.Itoa(value) }
func formatInt64(value int64) string  { return strconv.FormatInt(value, 10) }
func formatBool(value bool) string    { return strconv.FormatBool(value) }
func formatQuote(value string) string { return strconv.Quote(value) }
