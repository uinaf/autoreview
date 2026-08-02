package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"github.com/uinaf/autoreview/internal/protocol"
)

func WriteJSON(output io.Writer, value protocol.Report) error {
	if err := value.Validate(); err != nil {
		return fmt.Errorf("validate report: %w", err)
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func WriteTerminal(output io.Writer, value protocol.Report) error {
	if err := value.Validate(); err != nil {
		return fmt.Errorf("validate report: %w", err)
	}
	if _, err := fmt.Fprintf(output, "status: %s\n", value.Status); err != nil {
		return err
	}
	if value.Status == protocol.StatusFailure {
		if _, err := fmt.Fprintf(output, "failure: %s: %s\n", value.Failure.Class, escape(value.Failure.Message, false)); err != nil {
			return err
		}
		return writeMetadata(output, value.Metadata)
	}

	if value.Status == protocol.StatusFindings {
		if _, err := fmt.Fprintf(output, "findings: %d\n", len(value.Review.Findings)); err != nil {
			return err
		}
		for index, finding := range value.Review.Findings {
			if _, err := fmt.Fprintf(output, "\n%d. [%s] %s\n   %s:%d-%d category=%s confidence=%.2f\n   %s\n",
				index+1,
				finding.Priority,
				escape(finding.Title, false),
				escape(finding.Location.FilePath, false),
				finding.Location.StartLine,
				finding.Location.EndLine,
				finding.Category,
				finding.Confidence,
				indent(escape(finding.Body, true)),
			); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintf(output, "summary: %s\nconfidence: %.2f\n", indent(escape(value.Review.OverallExplanation, true)), value.Review.OverallConfidence); err != nil {
		return err
	}
	return writeMetadata(output, value.Metadata)
}

func writeMetadata(output io.Writer, metadata protocol.Metadata) error {
	if metadata.Provider != nil {
		if _, err := fmt.Fprintf(output, "provider: %s model=%s version=%s\n", metadata.Provider.Name, escape(metadata.Provider.Model, false), escape(metadata.Provider.Version, false)); err != nil {
			return err
		}
	}
	if metadata.Target != nil {
		if _, err := fmt.Fprintf(output, "target: %s snapshot=%s files=%d\n", metadata.Target.Mode, escape(metadata.Target.SnapshotHash, false), len(metadata.Target.Files)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(output, "attempts: %d duration_ms=%d\n", len(metadata.Attempts), metadata.DurationMS)
	return err
}

func indent(value string) string {
	return strings.ReplaceAll(value, "\n", "\n   ")
}

func escape(value string, multiline bool) string {
	var result strings.Builder
	for _, character := range value {
		if multiline && (character == '\n' || character == '\t') {
			result.WriteRune(character)
			continue
		}
		if unicode.IsControl(character) || unicode.In(character, unicode.Cf) {
			if character <= 0xffff {
				result.WriteString(fmt.Sprintf("\\u%04x", character))
			} else {
				result.WriteString(strconv.QuoteRuneToASCII(character))
			}
			continue
		}
		result.WriteRune(character)
	}
	return result.String()
}
