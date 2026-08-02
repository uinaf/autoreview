package provider

import (
	"context"
	"time"

	"github.com/uinaf/autoreview/internal/config"
	"github.com/uinaf/autoreview/internal/protocol"
)

const DefaultCodexModel = "gpt-5.6-sol"

type Request struct {
	Prompt string
	Config config.Effective
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
