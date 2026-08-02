package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

func DecodeReport(data []byte) (Report, error) {
	if !utf8.Valid(data) {
		return Report{}, fmt.Errorf("report contains invalid UTF-8")
	}
	if err := validateReportShape(data); err != nil {
		return Report{}, err
	}

	var report Report
	if err := decodeOne(data, &report); err != nil {
		return Report{}, fmt.Errorf("decode report: %w", err)
	}
	if err := report.Validate(); err != nil {
		return Report{}, err
	}
	return report, nil
}

func DecodeReview(data []byte) (Review, error) {
	if !utf8.Valid(data) {
		return Review{}, fmt.Errorf("review contains invalid UTF-8")
	}
	if err := validateReviewShape(data, "review"); err != nil {
		return Review{}, err
	}

	var review Review
	if err := decodeOne(data, &review); err != nil {
		return Review{}, fmt.Errorf("decode review: %w", err)
	}
	if err := review.Validate(); err != nil {
		return Review{}, err
	}
	return review, nil
}

func decodeOne(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateReportShape(data []byte) error {
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	root, err := object(data, "report", "schema_version", "status", "review", "failure", "metadata")
	if err != nil {
		return err
	}
	if err := nonNull(root, "report", "schema_version", "status", "metadata"); err != nil {
		return err
	}
	if !isNull(root["review"]) {
		if err := validateReviewShape(root["review"], "report.review"); err != nil {
			return err
		}
	}
	if !isNull(root["failure"]) {
		failure, err := object(root["failure"], "report.failure", "class", "message")
		if err != nil {
			return err
		}
		if err := nonNull(failure, "report.failure", "class", "message"); err != nil {
			return err
		}
	}
	return validateMetadataShape(root["metadata"])
}

func validateReviewShape(data []byte, path string) error {
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	review, err := object(data, path, "findings", "overall_explanation", "overall_confidence")
	if err != nil {
		return err
	}
	if err := nonNull(review, path, "findings", "overall_explanation", "overall_confidence"); err != nil {
		return err
	}
	var findings []json.RawMessage
	if err := json.Unmarshal(review["findings"], &findings); err != nil {
		return fmt.Errorf("%s.findings must be an array: %w", path, err)
	}
	for index, raw := range findings {
		findingPath := fmt.Sprintf("%s.findings[%d]", path, index)
		finding, err := object(raw, findingPath, "title", "body", "priority", "confidence", "category", "location")
		if err != nil {
			return err
		}
		if err := nonNull(finding, findingPath, "title", "body", "priority", "confidence", "category", "location"); err != nil {
			return err
		}
		location, err := object(finding["location"], findingPath+".location", "file_path", "start_line", "end_line")
		if err != nil {
			return err
		}
		if err := nonNull(location, findingPath+".location", "file_path", "start_line", "end_line"); err != nil {
			return err
		}
	}
	return nil
}

func validateMetadataShape(data []byte) error {
	metadata, err := object(data, "report.metadata", "target", "provider", "attempts", "duration_ms", "isolation", "web_access", "protocol_recovery")
	if err != nil {
		return err
	}
	if err := nonNull(metadata, "report.metadata", "attempts", "duration_ms", "web_access", "protocol_recovery"); err != nil {
		return err
	}
	if !isNull(metadata["target"]) {
		target, err := object(metadata["target"], "report.metadata.target", "mode", "snapshot_hash", "head_revision", "base_revision", "commit_revision", "files")
		if err != nil {
			return err
		}
		if err := nonNull(target, "report.metadata.target", "mode", "snapshot_hash", "head_revision", "base_revision", "commit_revision", "files"); err != nil {
			return err
		}
		var files []json.RawMessage
		if err := json.Unmarshal(target["files"], &files); err != nil {
			return fmt.Errorf("report.metadata.target.files must be an array: %w", err)
		}
		for fileIndex, raw := range files {
			filePath := fmt.Sprintf("report.metadata.target.files[%d]", fileIndex)
			file, err := object(raw, filePath, "file_path", "line_ranges")
			if err != nil {
				return err
			}
			if err := nonNull(file, filePath, "file_path", "line_ranges"); err != nil {
				return err
			}
			var ranges []json.RawMessage
			if err := json.Unmarshal(file["line_ranges"], &ranges); err != nil {
				return fmt.Errorf("%s.line_ranges must be an array: %w", filePath, err)
			}
			for rangeIndex, rangeRaw := range ranges {
				rangePath := fmt.Sprintf("%s.line_ranges[%d]", filePath, rangeIndex)
				lineRange, err := object(rangeRaw, rangePath, "start_line", "end_line")
				if err != nil {
					return err
				}
				if err := nonNull(lineRange, rangePath, "start_line", "end_line"); err != nil {
					return err
				}
			}
		}
	}
	if !isNull(metadata["provider"]) {
		provider, err := object(metadata["provider"], "report.metadata.provider", "name", "model", "version")
		if err != nil {
			return err
		}
		if err := nonNull(provider, "report.metadata.provider", "name", "model", "version"); err != nil {
			return err
		}
	}
	var attempts []json.RawMessage
	if err := json.Unmarshal(metadata["attempts"], &attempts); err != nil {
		return fmt.Errorf("report.metadata.attempts must be an array: %w", err)
	}
	for index, raw := range attempts {
		attemptPath := fmt.Sprintf("report.metadata.attempts[%d]", index)
		attempt, err := object(raw, attemptPath, "number", "outcome", "duration_ms", "error_class")
		if err != nil {
			return err
		}
		if err := nonNull(attempt, attemptPath, "number", "outcome", "duration_ms"); err != nil {
			return err
		}
	}
	recovery, err := object(metadata["protocol_recovery"], "report.metadata.protocol_recovery", "applied", "strategy")
	if err != nil {
		return err
	}
	if err := nonNull(recovery, "report.metadata.protocol_recovery", "applied"); err != nil {
		return err
	}
	return nil
}

func object(data []byte, path string, fields ...string) (map[string]json.RawMessage, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("%s must be an object: %w", path, err)
	}
	if value == nil {
		return nil, fmt.Errorf("%s must be an object", path)
	}
	allowed := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		allowed[field] = struct{}{}
		if _, ok := value[field]; !ok {
			return nil, fmt.Errorf("%s missing required field %q", path, field)
		}
	}
	for field := range value {
		if _, ok := allowed[field]; !ok {
			return nil, fmt.Errorf("%s has unknown field %q", path, field)
		}
	}
	return value, nil
}

func isNull(data []byte) bool {
	return bytes.Equal(bytes.TrimSpace(data), []byte("null"))
}

func nonNull(value map[string]json.RawMessage, path string, fields ...string) error {
	for _, field := range fields {
		if isNull(value[field]) {
			return fmt.Errorf("%s.%s must not be null", path, field)
		}
	}
	return nil
}

func rejectDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := walkJSONValue(decoder, "$"); err != nil {
		return err
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected JSON token after root value: %v", token)
		}
		return fmt.Errorf("decode JSON: %w", err)
	}
	return nil
}

func walkJSONValue(decoder *json.Decoder, currentPath string) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode JSON at %s: %w", currentPath, err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("decode JSON object at %s: %w", currentPath, err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("decode JSON object at %s: key is not a string", currentPath)
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate field %q at %s", key, currentPath)
			}
			seen[key] = struct{}{}
			if err := walkJSONValue(decoder, currentPath+"."+key); err != nil {
				return err
			}
		}
	case '[':
		for index := 0; decoder.More(); index++ {
			if err := walkJSONValue(decoder, fmt.Sprintf("%s[%d]", currentPath, index)); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q at %s", delimiter, currentPath)
	}
	closing, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode JSON at %s: %w", currentPath, err)
	}
	expected := json.Delim('}')
	if delimiter == '[' {
		expected = ']'
	}
	if closing != expected {
		return fmt.Errorf("unexpected JSON delimiter %q at %s", closing, currentPath)
	}
	return nil
}
