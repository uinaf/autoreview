package provider

import (
	"context"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uinaf/autoreview/internal/config"
	"github.com/uinaf/autoreview/internal/protocol"
)

const (
	DefaultCodexModel  = "gpt-5.6-sol"
	DefaultClaudeModel = "claude-opus-5"
	DefaultCursorModel = "cursor-grok-4.5-high-fast"
)

type Request struct {
	Prompt        string
	TrustedSuffix string
	Config        config.Effective
}

func (request Request) validPrompt() bool {
	return utf8.ValidString(request.Prompt) && utf8.ValidString(request.TrustedSuffix) && strings.TrimSpace(request.Prompt) != ""
}

func (request Request) promptBytes() (int64, bool) {
	if len(request.TrustedSuffix) > int(^uint(0)>>1)-len(request.Prompt) {
		return 0, false
	}
	return int64(len(request.Prompt) + len(request.TrustedSuffix)), true
}

func (request Request) promptReader(additionalSuffix string) io.Reader {
	readers := []io.Reader{strings.NewReader(request.Prompt)}
	if request.TrustedSuffix != "" {
		readers = append(readers, strings.NewReader(request.TrustedSuffix))
	}
	if additionalSuffix != "" {
		readers = append(readers, strings.NewReader(additionalSuffix))
	}
	if len(readers) == 1 {
		return readers[0]
	}
	return io.MultiReader(readers...)
}

type Result struct {
	Review           protocol.Review
	Provider         protocol.Provider
	Attempt          protocol.Attempt
	Duration         time.Duration
	Isolation        protocol.Isolation
	WebAccess        bool
	ProtocolRecovery protocol.ProtocolRecovery
}

type Reviewer interface {
	Review(context.Context, Request) (Result, error)
}

type Error struct {
	Class   protocol.FailureClass
	Message string
	Attempt *protocol.Attempt
}

func (failure *Error) Error() string {
	return failure.Message
}
