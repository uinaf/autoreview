package protocol

import (
	"fmt"
	"math"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (report Report) Validate() error {
	if report.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %q", report.SchemaVersion)
	}
	if report.Metadata.DurationMS < 0 {
		return fmt.Errorf("metadata.duration_ms must be non-negative")
	}
	if err := report.Metadata.ProtocolRecovery.validate(); err != nil {
		return err
	}
	if report.Metadata.Isolation != nil && !validIsolation(*report.Metadata.Isolation) {
		return fmt.Errorf("invalid metadata.isolation %q", *report.Metadata.Isolation)
	}
	if report.Metadata.Target != nil {
		if err := report.Metadata.Target.validate(); err != nil {
			return err
		}
	}
	if report.Metadata.Provider != nil {
		if err := report.Metadata.Provider.validate(); err != nil {
			return err
		}
	}
	if report.Metadata.ProtocolRecovery.Applied && (report.Metadata.Provider == nil || report.Metadata.Provider.Name != ProviderCursor) {
		return fmt.Errorf("cursor protocol recovery requires provider cursor")
	}
	if err := validateAttempts(report.Metadata.Attempts); err != nil {
		return err
	}

	switch report.Status {
	case StatusClean, StatusFindings:
		if report.Review == nil || report.Failure != nil {
			return fmt.Errorf("status %q requires review and forbids failure", report.Status)
		}
		if report.Metadata.Target == nil || report.Metadata.Provider == nil || report.Metadata.Isolation == nil {
			return fmt.Errorf("status %q requires target, provider, and isolation metadata", report.Status)
		}
		if len(report.Metadata.Attempts) == 0 || report.Metadata.Attempts[len(report.Metadata.Attempts)-1].Outcome != AttemptValid {
			return fmt.Errorf("status %q requires a final valid attempt", report.Status)
		}
		if err := report.Review.Validate(); err != nil {
			return err
		}
		if report.Status == StatusClean && len(report.Review.Findings) != 0 {
			return fmt.Errorf("clean status requires zero findings")
		}
		if report.Status == StatusFindings && len(report.Review.Findings) == 0 {
			return fmt.Errorf("findings status requires at least one finding")
		}
		if err := validateFindingBoundaries(report.Review.Findings, report.Metadata.Target.Files); err != nil {
			return err
		}
	case StatusFailure:
		if report.Failure == nil || report.Review != nil {
			return fmt.Errorf("failure status requires failure and forbids review")
		}
		if err := report.Failure.validate(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid status %q", report.Status)
	}
	return nil
}

func (review Review) Validate() error {
	if review.Findings == nil {
		return fmt.Errorf("findings must be an array")
	}
	if err := boundedText("overall_explanation", review.OverallExplanation, 3000); err != nil {
		return err
	}
	if !confidence(review.OverallConfidence) {
		return fmt.Errorf("overall_confidence must be between 0 and 1")
	}
	for index, finding := range review.Findings {
		if err := finding.validate(); err != nil {
			return fmt.Errorf("finding %d: %w", index, err)
		}
	}
	return nil
}

func (finding Finding) validate() error {
	if err := boundedText("title", finding.Title, 140); err != nil {
		return err
	}
	if err := boundedText("body", finding.Body, 2000); err != nil {
		return err
	}
	switch finding.Priority {
	case PriorityP0, PriorityP1, PriorityP2, PriorityP3:
	default:
		return fmt.Errorf("invalid priority %q", finding.Priority)
	}
	if !confidence(finding.Confidence) {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	switch finding.Category {
	case CategoryBug, CategorySecurity, CategoryRegression, CategoryTestGap, CategoryMaintainability:
	default:
		return fmt.Errorf("invalid category %q", finding.Category)
	}
	if err := validPath(finding.Location.FilePath); err != nil {
		return fmt.Errorf("location.file_path: %w", err)
	}
	if finding.Location.StartLine < 1 || finding.Location.EndLine < finding.Location.StartLine {
		return fmt.Errorf("invalid location line range %d-%d", finding.Location.StartLine, finding.Location.EndLine)
	}
	if finding.Location.EndLine > MaxLineNumber {
		return fmt.Errorf("location line numbers must not exceed %d", MaxLineNumber)
	}
	return nil
}

func (failure Failure) validate() error {
	if !validFailureClass(failure.Class) {
		return fmt.Errorf("invalid failure.class %q", failure.Class)
	}
	return boundedText("failure.message", failure.Message, 2000)
}

func (target Target) validate() error {
	switch target.Mode {
	case TargetLocal:
		if target.BaseRevision != "" || target.CommitRevision != "" {
			return fmt.Errorf("local target requires head_revision and forbids base_revision and commit_revision")
		}
		if err := boundedText("target.head_revision", target.HeadRevision, 4096); err != nil {
			return err
		}
	case TargetBranch:
		if target.CommitRevision != "" {
			return fmt.Errorf("branch target requires base_revision and head_revision and forbids commit_revision")
		}
		if err := boundedText("target.base_revision", target.BaseRevision, 4096); err != nil {
			return err
		}
		if err := boundedText("target.head_revision", target.HeadRevision, 4096); err != nil {
			return err
		}
	case TargetCommit:
		if target.BaseRevision != "" || target.HeadRevision != "" {
			return fmt.Errorf("commit target requires commit_revision and forbids base_revision and head_revision")
		}
		if err := boundedText("target.commit_revision", target.CommitRevision, 4096); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid target.mode %q", target.Mode)
	}
	if err := boundedText("target.snapshot_hash", target.SnapshotHash, 512); err != nil {
		return err
	}
	if len(target.Files) == 0 {
		return fmt.Errorf("target.files must not be empty")
	}
	seen := make(map[string]struct{}, len(target.Files))
	for index, file := range target.Files {
		if err := validPath(file.FilePath); err != nil {
			return fmt.Errorf("target.files[%d].file_path: %w", index, err)
		}
		if _, ok := seen[file.FilePath]; ok {
			return fmt.Errorf("target.files contains duplicate path %q", file.FilePath)
		}
		seen[file.FilePath] = struct{}{}
		if len(file.LineRanges) == 0 {
			return fmt.Errorf("target file %q has no line ranges", file.FilePath)
		}
		lastEnd := 0
		for _, lineRange := range file.LineRanges {
			if lineRange.StartLine < 1 || lineRange.EndLine < lineRange.StartLine {
				return fmt.Errorf("target file %q has invalid line range %d-%d", file.FilePath, lineRange.StartLine, lineRange.EndLine)
			}
			if lineRange.EndLine > MaxLineNumber {
				return fmt.Errorf("target file %q line numbers must not exceed %d", file.FilePath, MaxLineNumber)
			}
			if lineRange.StartLine <= lastEnd {
				return fmt.Errorf("target file %q has overlapping or unsorted line ranges", file.FilePath)
			}
			lastEnd = lineRange.EndLine
		}
	}
	return nil
}

func (provider Provider) validate() error {
	switch provider.Name {
	case ProviderCodex, ProviderClaude, ProviderCursor:
	default:
		return fmt.Errorf("invalid provider.name %q", provider.Name)
	}
	if err := boundedText("provider.model", provider.Model, 200); err != nil {
		return err
	}
	return boundedText("provider.version", provider.Version, 200)
}

func validateAttempts(attempts []Attempt) error {
	if attempts == nil {
		return fmt.Errorf("metadata.attempts must be an array")
	}
	for index, attempt := range attempts {
		if attempt.Number > MaxAttemptNumber {
			return fmt.Errorf("attempt number must not exceed %d", MaxAttemptNumber)
		}
		if attempt.Number != index+1 {
			return fmt.Errorf("attempt %d has non-sequential number %d", index+1, attempt.Number)
		}
		if attempt.DurationMS < 0 {
			return fmt.Errorf("attempt %d duration_ms must be non-negative", attempt.Number)
		}
		switch attempt.Outcome {
		case AttemptValid:
			if attempt.ErrorClass != nil {
				return fmt.Errorf("valid attempt %d forbids error_class", attempt.Number)
			}
		case AttemptMalformed, AttemptFailed:
			if attempt.ErrorClass == nil || !validFailureClass(*attempt.ErrorClass) {
				return fmt.Errorf("non-valid attempt %d requires valid error_class", attempt.Number)
			}
		default:
			return fmt.Errorf("attempt %d has invalid outcome %q", attempt.Number, attempt.Outcome)
		}
	}
	return nil
}

func (recovery ProtocolRecovery) validate() error {
	if !recovery.Applied {
		if recovery.Strategy != nil {
			return fmt.Errorf("protocol_recovery.strategy requires applied=true")
		}
		return nil
	}
	if recovery.Strategy == nil || *recovery.Strategy != RecoveryCursorTrailingObject {
		return fmt.Errorf("protocol_recovery has invalid strategy")
	}
	return nil
}

func validateFindingBoundaries(findings []Finding, files []ReviewedFile) error {
	byPath := make(map[string][]LineRange, len(files))
	for _, file := range files {
		byPath[file.FilePath] = file.LineRanges
	}
	for index, finding := range findings {
		ranges, ok := byPath[finding.Location.FilePath]
		if !ok {
			return fmt.Errorf("finding %d references unreviewed file %q", index, finding.Location.FilePath)
		}
		inside := false
		for _, lineRange := range ranges {
			if finding.Location.StartLine >= lineRange.StartLine && finding.Location.EndLine <= lineRange.EndLine {
				inside = true
				break
			}
		}
		if !inside {
			return fmt.Errorf("finding %d references unreviewed lines %s:%d-%d", index, finding.Location.FilePath, finding.Location.StartLine, finding.Location.EndLine)
		}
	}
	return nil
}

func boundedText(name, value string, limit int) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must contain valid UTF-8", name)
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must be non-empty", name)
	}
	if utf8.RuneCountInString(value) > limit {
		return fmt.Errorf("%s exceeds %d characters", name, limit)
	}
	return nil
}

func confidence(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func validPath(value string) error {
	volumePath := len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':'
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > MaxPathCharacters || strings.TrimSpace(value) == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") || volumePath || path.Clean(value) != value || value == "." {
		return fmt.Errorf("must be a normalized repository-relative POSIX path")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("must not contain control characters")
		}
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return fmt.Errorf("must not traverse outside the repository")
		}
	}
	return nil
}

func validIsolation(value Isolation) bool {
	return value == IsolationStrict || value == IsolationNative
}

func validFailureClass(value FailureClass) bool {
	switch value {
	case FailureConfig, FailureTarget, FailureSecretScan, FailureCapability, FailureAuth, FailureTimeout, FailureCancelled, FailureProvider, FailureProtocol, FailureSourceChanged, FailureInternal:
		return true
	default:
		return false
	}
}
