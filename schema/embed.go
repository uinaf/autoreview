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
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode Codex review schema: %w", err)
	}
	return encoded, nil
}
