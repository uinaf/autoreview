package schema

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed review-v1.schema.json
var reviewV1 []byte

func ReviewV1() []byte {
	return append([]byte(nil), reviewV1...)
}

func CodexReviewV1() ([]byte, error) {
	return providerReviewV1("Codex", false, false)
}

func ClaudeReviewV1() ([]byte, error) {
	return providerReviewV1("Claude", true, false)
}

func GrokReviewV1() ([]byte, error) {
	return providerReviewV1("Grok", true, true)
}

func providerReviewV1(provider string, omitDraftURI, omitNonWhitespacePattern bool) ([]byte, error) {
	var document map[string]any
	if err := json.Unmarshal(reviewV1, &document); err != nil {
		return nil, fmt.Errorf("decode embedded review schema: %w", err)
	}
	definitions, ok := document["$defs"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("review schema is missing $defs")
	}
	relativePath, ok := definitions["relative_path"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("review schema is missing relative_path")
	}
	delete(relativePath, "not")
	if omitDraftURI {
		delete(document, "$schema")
	}
	if omitNonWhitespacePattern {
		deletePattern(document, `\S`)
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode %s review schema: %w", provider, err)
	}
	return encoded, nil
}

func deletePattern(value any, pattern string) {
	switch value := value.(type) {
	case map[string]any:
		if value["pattern"] == pattern {
			delete(value, "pattern")
		}
		for _, child := range value {
			deletePattern(child, pattern)
		}
	case []any:
		for _, child := range value {
			deletePattern(child, pattern)
		}
	}
}
