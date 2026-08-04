package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/uinaf/autoreview/internal/protocol"
	"github.com/uinaf/autoreview/internal/target"
	"github.com/uinaf/autoreview/internal/trustedexec"
	"go.yaml.in/yaml/v3"
)

const maximumConfigBytes = int64(64 << 10)

type Options struct {
	Repository string
	GitPath    string
	Overrides  Overrides
	LookupEnv  func(string) (string, bool)
	HomeDir    func() (string, error)
}

type rawConfig struct {
	Engine          yamlString `yaml:"engine"`
	Model           yamlString `yaml:"model"`
	ReasoningEffort yamlString `yaml:"reasoning_effort"`
	Timeout         yamlString `yaml:"timeout"`
	Retries         yamlInt    `yaml:"retries"`
	MaxBytes        yamlInt    `yaml:"max_bytes"`
	Isolation       yamlString `yaml:"isolation"`
	WebAccess       yamlBool   `yaml:"web_access"`
}

type yamlString struct {
	value string
	set   bool
}

func (value *yamlString) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return fmt.Errorf("must be a string, got %s", node.ShortTag())
	}
	value.value = node.Value
	value.set = true
	return nil
}

type yamlInt struct {
	value int64
	set   bool
}

func (value *yamlInt) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
		return fmt.Errorf("must be an integer, got %s", node.ShortTag())
	}
	parsed, err := strconv.ParseInt(node.Value, 0, 64)
	if err != nil {
		return fmt.Errorf("invalid integer: %w", err)
	}
	value.value = parsed
	value.set = true
	return nil
}

type yamlBool struct {
	value bool
	set   bool
}

func (value *yamlBool) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
		return fmt.Errorf("must be a boolean, got %s", node.ShortTag())
	}
	parsed, err := strconv.ParseBool(node.Value)
	if err != nil {
		return fmt.Errorf("invalid boolean: %w", err)
	}
	value.value = parsed
	value.set = true
	return nil
}

func Load(ctx context.Context, options Options) (Effective, error) {
	lookup := options.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	homeDir := options.HomeDir
	if homeDir == nil {
		homeDir = accountHomeDir
	}
	repository := options.Repository
	if repository == "" {
		repository = "."
	}
	root, err := repositoryRoot(ctx, repository, options.GitPath)
	if err != nil {
		return Effective{}, err
	}
	effective := defaults()
	xdgPath, trustedRoot, trustedCapabilities, err := resolveXDGPath(lookup, homeDir)
	if err != nil {
		return Effective{}, err
	}
	if err := applyFile(&effective, xdgPath, SourceXDG, trustedCapabilities, trustedRoot, root); err != nil {
		return Effective{}, err
	}
	if err := applyFile(&effective, filepath.Join(root, ".autoreview.yaml"), SourceRepository, false, "", root); err != nil {
		return Effective{}, err
	}
	if err := applyEnvironment(&effective, lookup); err != nil {
		return Effective{}, err
	}
	if err := applyOverrides(&effective, options.Overrides); err != nil {
		return Effective{}, err
	}
	applyProviderDefaults(&effective)
	if err := effective.Validate(); err != nil {
		return Effective{}, err
	}
	return effective, nil
}

func accountHomeDir() (string, error) {
	current, err := user.Current()
	if err != nil {
		return "", err
	}
	return current.HomeDir, nil
}

func resolveXDGPath(lookup func(string) (string, bool), homeDir func() (string, error)) (string, string, bool, error) {
	if value, ok := lookup("XDG_CONFIG_HOME"); ok && value != "" {
		if !filepath.IsAbs(value) {
			return "", "", false, fmt.Errorf("XDG_CONFIG_HOME must be absolute")
		}
		return filepath.Join(value, "autoreview", "config.yaml"), "", false, nil
	}
	home, err := homeDir()
	if err != nil {
		return "", "", false, fmt.Errorf("resolve home directory for XDG config: %w", err)
	}
	if !filepath.IsAbs(home) {
		return "", "", false, fmt.Errorf("home directory for XDG config must be absolute")
	}
	return filepath.Join(home, ".config", "autoreview", "config.yaml"), home, true, nil
}

func applyFile(effective *Effective, path string, source Source, allowCapabilities bool, trustedRoot, repositoryPath string) error {
	var content []byte
	var err error
	if source == SourceXDG && allowCapabilities {
		content, err = readTrustedConfigFile(path, trustedRoot, repositoryPath)
	} else {
		content, err = readUntrustedConfigFile(path)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s config %q: %w", source, path, err)
	}
	if err := validateYAMLDocument(content); err != nil {
		return fmt.Errorf("%s config %q: %w", source, path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	var raw rawConfig
	if err := decoder.Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s config %q: %w", source, path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%s config %q: multiple YAML documents are unsupported", source, path)
		}
		return fmt.Errorf("%s config %q: %w", source, path, err)
	}
	return applyRaw(effective, raw, source, allowCapabilities)
}

func validateYAMLDocument(content []byte) error {
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	var document yaml.Node
	if err := decoder.Decode(&document); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("config document must be a mapping")
	}
	expected := map[string]string{
		"engine": "!!str", "model": "!!str", "reasoning_effort": "!!str", "timeout": "!!str",
		"retries": "!!int", "max_bytes": "!!int", "isolation": "!!str", "web_access": "!!bool",
	}
	seen := map[string]struct{}{}
	mapping := document.Content[0]
	for index := 0; index < len(mapping.Content); index += 2 {
		key := mapping.Content[index]
		value := mapping.Content[index+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return fmt.Errorf("config keys must be strings")
		}
		want, ok := expected[key.Value]
		if !ok {
			return fmt.Errorf("unknown config key %q", key.Value)
		}
		if _, duplicate := seen[key.Value]; duplicate {
			return fmt.Errorf("duplicate config key %q", key.Value)
		}
		seen[key.Value] = struct{}{}
		if value.Kind != yaml.ScalarNode || value.Tag != want {
			return fmt.Errorf("config key %q must be %s, got %s", key.Value, want, value.ShortTag())
		}
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple YAML documents are unsupported")
		}
		return err
	}
	return nil
}

func applyEnvironment(effective *Effective, lookup func(string) (string, bool)) error {
	raw := rawConfig{}
	if value, ok := lookup("AUTOREVIEW_ENGINE"); ok {
		raw.Engine = yamlString{value: value, set: true}
	}
	if value, ok := lookup("AUTOREVIEW_MODEL"); ok {
		raw.Model = yamlString{value: value, set: true}
	}
	if value, ok := lookup("AUTOREVIEW_REASONING_EFFORT"); ok {
		raw.ReasoningEffort = yamlString{value: value, set: true}
	}
	if value, ok := lookup("AUTOREVIEW_TIMEOUT"); ok {
		raw.Timeout = yamlString{value: value, set: true}
	}
	if value, ok := lookup("AUTOREVIEW_RETRIES"); ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("environment AUTOREVIEW_RETRIES: %w", err)
		}
		raw.Retries = yamlInt{value: int64(parsed), set: true}
	}
	if value, ok := lookup("AUTOREVIEW_MAX_BYTES"); ok {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("environment AUTOREVIEW_MAX_BYTES: %w", err)
		}
		raw.MaxBytes = yamlInt{value: parsed, set: true}
	}
	if value, ok := lookup("AUTOREVIEW_ISOLATION"); ok {
		raw.Isolation = yamlString{value: value, set: true}
	}
	if value, ok := lookup("AUTOREVIEW_WEB_ACCESS"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("environment AUTOREVIEW_WEB_ACCESS: %w", err)
		}
		raw.WebAccess = yamlBool{value: parsed, set: true}
	}
	return applyRaw(effective, raw, SourceEnvironment, false)
}

func applyOverrides(effective *Effective, overrides Overrides) error {
	raw := rawConfig{}
	if overrides.Engine != nil {
		raw.Engine = yamlString{value: string(*overrides.Engine), set: true}
	}
	if overrides.Model != nil {
		raw.Model = yamlString{value: *overrides.Model, set: true}
	}
	if overrides.ReasoningEffort != nil {
		raw.ReasoningEffort = yamlString{value: string(*overrides.ReasoningEffort), set: true}
	}
	if overrides.Timeout != nil {
		raw.Timeout = yamlString{value: overrides.Timeout.String(), set: true}
	}
	if overrides.Retries != nil {
		raw.Retries = yamlInt{value: int64(*overrides.Retries), set: true}
	}
	if overrides.MaxBytes != nil {
		raw.MaxBytes = yamlInt{value: *overrides.MaxBytes, set: true}
	}
	if overrides.Isolation != nil {
		raw.Isolation = yamlString{value: string(*overrides.Isolation), set: true}
	}
	if overrides.WebAccess != nil {
		raw.WebAccess = yamlBool{value: *overrides.WebAccess, set: true}
	}
	return applyRaw(effective, raw, SourceFlag, true)
}

func applyRaw(effective *Effective, raw rawConfig, source Source, allowCapabilities bool) error {
	if err := validateRaw(raw, source); err != nil {
		return err
	}
	if raw.Isolation.set && protocol.Isolation(raw.Isolation.value) == protocol.IsolationNative && !allowCapabilities && effective.Isolation.Value == protocol.IsolationStrict {
		return fmt.Errorf("%s config cannot weaken strict isolation", source)
	}
	if raw.WebAccess.set && raw.WebAccess.value && !allowCapabilities {
		return fmt.Errorf("%s config cannot enable web access", source)
	}
	if raw.Engine.set {
		effective.Engine = Value[protocol.ProviderName]{Value: protocol.ProviderName(raw.Engine.value), Source: source}
	}
	if raw.Model.set {
		effective.Model = Value[string]{Value: raw.Model.value, Source: source}
	}
	if raw.ReasoningEffort.set {
		effective.ReasoningEffort = Value[ReasoningEffort]{Value: ReasoningEffort(raw.ReasoningEffort.value), Source: source}
	}
	if raw.Timeout.set {
		parsed, err := time.ParseDuration(raw.Timeout.value)
		if err != nil {
			return fmt.Errorf("%s timeout: %w", source, err)
		}
		effective.Timeout = Value[Duration]{Value: Duration(parsed), Source: source}
	}
	if raw.Retries.set {
		effective.Retries = Value[int]{Value: int(raw.Retries.value), Source: source}
	}
	if raw.MaxBytes.set {
		effective.MaxBytes = Value[int64]{Value: raw.MaxBytes.value, Source: source}
	}
	if raw.Isolation.set {
		isolation := protocol.Isolation(raw.Isolation.value)
		effective.Isolation = Value[protocol.Isolation]{Value: isolation, Source: source}
	}
	if raw.WebAccess.set {
		effective.WebAccess = Value[bool]{Value: raw.WebAccess.value, Source: source}
	}
	return nil
}

func applyProviderDefaults(effective *Effective) {
	if effective.Engine.Value == protocol.ProviderCursor && effective.Engine.Source == SourceFlag && effective.WebAccess.Source == SourceDefault {
		effective.WebAccess.Value = true
		effective.WebAccess.Source = SourceFlag
	}
}

func validateRaw(raw rawConfig, source Source) error {
	if raw.Engine.set {
		switch protocol.ProviderName(raw.Engine.value) {
		case protocol.ProviderCodex, protocol.ProviderClaude, protocol.ProviderCursor:
		default:
			return fmt.Errorf("%s engine: invalid value %q", source, raw.Engine.value)
		}
	}
	if raw.Model.set {
		if err := optionalText("model", raw.Model.value, 200); err != nil {
			return fmt.Errorf("%s model: %w", source, err)
		}
	}
	if raw.ReasoningEffort.set {
		switch ReasoningEffort(raw.ReasoningEffort.value) {
		case ReasoningMinimal, ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningXHigh, ReasoningMax, ReasoningUltra:
		default:
			return fmt.Errorf("%s reasoning_effort: invalid value %q", source, raw.ReasoningEffort.value)
		}
	}
	if raw.Timeout.set {
		parsed, err := time.ParseDuration(raw.Timeout.value)
		if err != nil {
			return fmt.Errorf("%s timeout: %w", source, err)
		}
		if parsed <= 0 || parsed > 24*time.Hour {
			return fmt.Errorf("%s timeout must be greater than zero and at most 24h", source)
		}
	}
	if raw.Retries.set && (raw.Retries.value < 0 || raw.Retries.value > 1) {
		return fmt.Errorf("%s retries must be 0 or 1 in v0.1", source)
	}
	if raw.MaxBytes.set && (raw.MaxBytes.value < 1 || raw.MaxBytes.value > target.MaximumMaxBytes) {
		return fmt.Errorf("%s max_bytes must be between 1 and %d", source, target.MaximumMaxBytes)
	}
	if raw.Isolation.set {
		isolation := protocol.Isolation(raw.Isolation.value)
		if isolation != protocol.IsolationStrict && isolation != protocol.IsolationNative {
			return fmt.Errorf("%s isolation: invalid value %q", source, raw.Isolation.value)
		}
	}
	return nil
}

func repositoryRoot(ctx context.Context, repository, gitPath string) (string, error) {
	gitPath, err := trustedexec.Resolve(
		ctx,
		"git",
		gitPath,
		repository,
		os.Environ(),
		trustedexec.GitProbe(os.TempDir()),
	)
	if err != nil {
		return "", fmt.Errorf("find git: %w", err)
	}
	absolute, err := filepath.Abs(repository)
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	command := exec.CommandContext(ctx, gitPath, "-C", absolute, "-c", "core.hooksPath=/dev/null", "rev-parse", "--show-toplevel")
	command.Dir = os.TempDir()
	command.Env = configGitEnvironment()
	output, err := command.Output()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	if err != nil {
		return "", fmt.Errorf("find repository root: %w", err)
	}
	root := strings.TrimSpace(string(output))
	if root == "" || !filepath.IsAbs(root) {
		return "", fmt.Errorf("git returned an invalid repository root")
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	requested, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve requested repository path: %w", err)
	}
	relative, err := filepath.Rel(resolved, requested)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("git worktree does not contain requested repository path")
	}
	return resolved, nil
}

func configGitEnvironment() []string {
	return trustedexec.GitEnvironment()
}
