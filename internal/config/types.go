package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/uinaf/autoreview/internal/protocol"
	"github.com/uinaf/autoreview/internal/target"
)

type Source string

const (
	SourceDefault     Source = "default"
	SourceXDG         Source = "xdg"
	SourceRepository  Source = "repository"
	SourceEnvironment Source = "environment"
	SourceFlag        Source = "flag"
)

type ReasoningEffort string

const (
	ReasoningMinimal ReasoningEffort = "minimal"
	ReasoningLow     ReasoningEffort = "low"
	ReasoningMedium  ReasoningEffort = "medium"
	ReasoningHigh    ReasoningEffort = "high"
	ReasoningXHigh   ReasoningEffort = "xhigh"
	ReasoningMax     ReasoningEffort = "max"
	ReasoningUltra   ReasoningEffort = "ultra"
)

type Duration time.Duration

func (duration Duration) String() string {
	return time.Duration(duration).String()
}

func (duration Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(duration.String())
}

func (duration *Duration) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	*duration = Duration(parsed)
	return nil
}

type Value[T any] struct {
	Value  T      `json:"value"`
	Source Source `json:"source"`
}

type Effective struct {
	Engine          Value[protocol.ProviderName] `json:"engine"`
	Model           Value[string]                `json:"model"`
	ReasoningEffort Value[ReasoningEffort]       `json:"reasoning_effort"`
	Timeout         Value[Duration]              `json:"timeout"`
	Retries         Value[int]                   `json:"retries"`
	MaxBytes        Value[int64]                 `json:"max_bytes"`
	Isolation       Value[protocol.Isolation]    `json:"isolation"`
	WebAccess       Value[bool]                  `json:"web_access"`
}

type Overrides struct {
	Engine          *protocol.ProviderName
	Model           *string
	ReasoningEffort *ReasoningEffort
	Timeout         *time.Duration
	Retries         *int
	MaxBytes        *int64
	Isolation       *protocol.Isolation
	WebAccess       *bool
}

func defaults() Effective {
	return Effective{
		Engine:          Value[protocol.ProviderName]{Source: SourceDefault},
		Model:           Value[string]{Source: SourceDefault},
		ReasoningEffort: Value[ReasoningEffort]{Value: ReasoningHigh, Source: SourceDefault},
		Timeout:         Value[Duration]{Value: Duration(15 * time.Minute), Source: SourceDefault},
		Retries:         Value[int]{Value: 1, Source: SourceDefault},
		MaxBytes:        Value[int64]{Value: target.DefaultMaxBytes, Source: SourceDefault},
		Isolation:       Value[protocol.Isolation]{Value: protocol.IsolationNative, Source: SourceDefault},
		WebAccess:       Value[bool]{Value: false, Source: SourceDefault},
	}
}

func (effective Effective) Validate() error {
	switch effective.Engine.Value {
	case protocol.ProviderCodex, protocol.ProviderClaude, protocol.ProviderCursor:
	case "":
		return fmt.Errorf("engine is required from a flag or configuration source")
	default:
		return fmt.Errorf("%s engine: invalid value %q", effective.Engine.Source, effective.Engine.Value)
	}
	if err := optionalText("model", effective.Model.Value, 200); err != nil {
		return fmt.Errorf("%s model: %w", effective.Model.Source, err)
	}
	switch effective.ReasoningEffort.Value {
	case ReasoningMinimal, ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningXHigh, ReasoningMax, ReasoningUltra:
	default:
		return fmt.Errorf("%s reasoning_effort: invalid value %q", effective.ReasoningEffort.Source, effective.ReasoningEffort.Value)
	}
	timeout := time.Duration(effective.Timeout.Value)
	if timeout <= 0 || timeout > 24*time.Hour {
		return fmt.Errorf("%s timeout must be greater than zero and at most 24h", effective.Timeout.Source)
	}
	if effective.Retries.Value < 0 || effective.Retries.Value > 1 {
		return fmt.Errorf("%s retries must be 0 or 1 in v0.1", effective.Retries.Source)
	}
	if effective.MaxBytes.Value < 1 || effective.MaxBytes.Value > target.MaximumMaxBytes {
		return fmt.Errorf("%s max_bytes must be between 1 and %d", effective.MaxBytes.Source, target.MaximumMaxBytes)
	}
	switch effective.Isolation.Value {
	case protocol.IsolationStrict, protocol.IsolationNative:
	default:
		return fmt.Errorf("%s isolation: invalid value %q", effective.Isolation.Source, effective.Isolation.Value)
	}
	return nil
}

func optionalText(name, value string, maximum int) error {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be valid trimmed UTF-8 of at most %d characters", name, maximum)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("%s must not contain control characters", name)
		}
	}
	return nil
}
