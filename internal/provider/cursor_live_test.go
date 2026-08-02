package provider

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/uinaf/autoreview/internal/config"
	"github.com/uinaf/autoreview/internal/protocol"
)

func TestCursorLive(t *testing.T) {
	if os.Getenv("AUTOREVIEW_TEST_LIVE_CURSOR") != "1" {
		t.Skip("set AUTOREVIEW_TEST_LIVE_CURSOR=1 to run the authenticated Cursor smoke test")
	}
	effective := cursorConfig(protocol.IsolationNative, true, 2*time.Minute)
	effective.Model = config.Value[string]{Source: config.SourceDefault}
	reviewer := NewCursor(CursorOptions{Repository: t.TempDir()})
	result, err := reviewer.Review(context.Background(), Request{
		Prompt: "Return exactly one review JSON object with no findings, overall_explanation set to Live Cursor adapter smoke passed., and overall_confidence set to 1. Do not use tools.",
		Config: effective,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider.Model != DefaultCursorModel || len(result.Review.Findings) != 0 {
		t.Fatalf("result = %+v", result)
	}
}
