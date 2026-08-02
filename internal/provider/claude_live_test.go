package provider

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/uinaf/autoreview/internal/config"
	"github.com/uinaf/autoreview/internal/protocol"
)

func TestClaudeLive(t *testing.T) {
	if os.Getenv("AUTOREVIEW_TEST_LIVE_CLAUDE") != "1" {
		t.Skip("set AUTOREVIEW_TEST_LIVE_CLAUDE=1 to run the authenticated Claude smoke test")
	}
	effective := claudeConfig(protocol.IsolationNative, false, 2*time.Minute)
	effective.Model = config.Value[string]{Source: config.SourceDefault}
	reviewer := NewClaude(ClaudeOptions{Repository: t.TempDir()})
	result, err := reviewer.Review(context.Background(), Request{
		Prompt: "Return one review object with no findings, overall_explanation set to Live Claude adapter smoke passed., and overall_confidence set to 1. Do not use tools.",
		Config: effective,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider.Model != DefaultClaudeModel || len(result.Review.Findings) != 0 {
		t.Fatalf("result = %+v", result)
	}
}
