package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/uinaf/autoreview/internal/config"
	"github.com/uinaf/autoreview/internal/protocol"
	"github.com/uinaf/autoreview/internal/provider"
	"github.com/uinaf/autoreview/internal/target"
)

const retryInstruction = "\nAUTOREVIEW-TRUSTED-PROTOCOL-RETRY-V1\nThe previous response did not satisfy the required review protocol. Return exactly one review object matching the supplied schema. Do not add prose, fences, or findings outside the frozen target boundaries.\n"

type ReviewerFactory func(protocol.ProviderName, string) provider.Reviewer

type Options struct {
	Collector   *target.Collector
	NewReviewer ReviewerFactory
	Repository  string
	Target      target.Request
	Config      config.Effective
	Progress    func(string)
	Now         func() time.Time
}

func Failure(class protocol.FailureClass, err error) protocol.Report {
	return failureReport(class, safeMessage(err), nil, []protocol.Attempt{}, 0)
}

func Run(ctx context.Context, options Options) protocol.Report {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	started := now()
	progress := options.Progress
	if progress == nil {
		progress = func(string) {}
	}
	failure := func(class protocol.FailureClass, err error, reviewedTarget *protocol.Target, attempts []protocol.Attempt) protocol.Report {
		return failureReport(class, safeMessage(err), reviewedTarget, attempts, elapsedMilliseconds(started, now()))
	}

	if options.Collector == nil {
		return failure(protocol.FailureInternal, errors.New("target collector is unavailable"), nil, []protocol.Attempt{})
	}
	if options.NewReviewer == nil {
		return failure(protocol.FailureInternal, errors.New("provider factory is unavailable"), nil, []protocol.Attempt{})
	}

	progress("collecting frozen target")
	bundle, err := options.Collector.Freeze(ctx, options.Repository, options.Target)
	if err != nil {
		return failure(classify(err), err, nil, []protocol.Attempt{})
	}
	reviewedTarget := bundle.Target()
	reviewer := options.NewReviewer(options.Config.Engine.Value, bundle.Repository())
	if reviewer == nil {
		return failure(protocol.FailureInternal, fmt.Errorf("provider %q is unavailable", options.Config.Engine.Value), &reviewedTarget, []protocol.Attempt{})
	}

	attempts := make([]protocol.Attempt, 0, options.Config.Retries.Value+1)
	prompt := string(bundle.Payload())
	for attemptNumber := 1; attemptNumber <= options.Config.Retries.Value+1; attemptNumber++ {
		if attemptNumber == 1 {
			progress("reviewing with " + string(options.Config.Engine.Value))
		} else {
			progress("retrying malformed provider response")
			prompt = string(bundle.Payload()) + retryInstruction
		}

		attemptStarted := now()
		result, reviewErr := reviewer.Review(ctx, provider.Request{Prompt: prompt, Config: options.Config})
		attemptDuration := elapsedMilliseconds(attemptStarted, now())

		if reviewErr != nil {
			class, attempt := providerFailure(reviewErr, attemptNumber, attemptDuration)
			if attempt != nil {
				attempts = append(attempts, *attempt)
			}
			if unchangedErr := bundle.VerifyUnchanged(ctx); unchangedErr != nil {
				return failure(classify(unchangedErr), unchangedErr, &reviewedTarget, attempts)
			}
			if class == protocol.FailureProtocol && attemptNumber <= options.Config.Retries.Value {
				if attempt == nil {
					attempts = append(attempts, malformedAttempt(attemptNumber, attemptDuration))
				}
				continue
			}
			return failure(class, reviewErr, &reviewedTarget, attempts)
		}

		attempt := normalizeAttempt(result.Attempt, attemptNumber, attemptDuration)
		attempts = append(attempts, attempt)
		if unchangedErr := bundle.VerifyUnchanged(ctx); unchangedErr != nil {
			return failure(classify(unchangedErr), unchangedErr, &reviewedTarget, attempts)
		}
		if result.Provider.Name != options.Config.Engine.Value || result.Isolation != options.Config.Isolation.Value || result.WebAccess != options.Config.WebAccess.Value {
			attempts[len(attempts)-1] = malformedAttempt(attemptNumber, attempt.DurationMS)
			if attemptNumber <= options.Config.Retries.Value {
				continue
			}
			return failure(protocol.FailureProtocol, errors.New("provider result metadata does not match the configured engine capabilities"), &reviewedTarget, attempts)
		}
		report := successReport(reviewedTarget, result, attempts, elapsedMilliseconds(started, now()))
		if validateErr := report.Validate(); validateErr != nil {
			attempts[len(attempts)-1] = malformedAttempt(attemptNumber, attempt.DurationMS)
			if attemptNumber <= options.Config.Retries.Value {
				continue
			}
			return failure(protocol.FailureProtocol, fmt.Errorf("provider result failed report validation: %w", validateErr), &reviewedTarget, attempts)
		}
		return report
	}

	return failure(protocol.FailureInternal, errors.New("review attempt loop ended unexpectedly"), &reviewedTarget, attempts)
}

func successReport(reviewedTarget protocol.Target, result provider.Result, attempts []protocol.Attempt, durationMS int64) protocol.Report {
	status := protocol.StatusClean
	if len(result.Review.Findings) > 0 {
		status = protocol.StatusFindings
	}
	isolation := result.Isolation
	providerMetadata := result.Provider
	return protocol.Report{
		SchemaVersion: protocol.SchemaVersion,
		Status:        status,
		Review:        &result.Review,
		Metadata: protocol.Metadata{
			Target:           &reviewedTarget,
			Provider:         &providerMetadata,
			Attempts:         append([]protocol.Attempt(nil), attempts...),
			DurationMS:       durationMS,
			Isolation:        &isolation,
			WebAccess:        result.WebAccess,
			ProtocolRecovery: result.ProtocolRecovery,
		},
	}
}

func failureReport(class protocol.FailureClass, message string, reviewedTarget *protocol.Target, attempts []protocol.Attempt, durationMS int64) protocol.Report {
	return protocol.Report{
		SchemaVersion: protocol.SchemaVersion,
		Status:        protocol.StatusFailure,
		Failure:       &protocol.Failure{Class: class, Message: message},
		Metadata: protocol.Metadata{
			Target:     reviewedTarget,
			Attempts:   append([]protocol.Attempt{}, attempts...),
			DurationMS: durationMS,
		},
	}
}

func providerFailure(err error, number int, durationMS int64) (protocol.FailureClass, *protocol.Attempt) {
	class := classifyReviewError(err)
	var failure *provider.Error
	if errors.As(err, &failure) {
		if failure.Attempt == nil {
			if class == protocol.FailureProtocol {
				attempt := malformedAttempt(number, durationMS)
				return class, &attempt
			}
			return class, nil
		}
		attempt := normalizeAttempt(*failure.Attempt, number, durationMS)
		if attempt.Outcome == protocol.AttemptValid {
			attempt.Outcome = protocol.AttemptFailed
		}
		attempt.ErrorClass = classPointer(class)
		return class, &attempt
	}
	outcome := protocol.AttemptFailed
	if class == protocol.FailureProtocol {
		outcome = protocol.AttemptMalformed
	}
	attempt := protocol.Attempt{Number: number, Outcome: outcome, DurationMS: durationMS, ErrorClass: classPointer(class)}
	return class, &attempt
}

func classifyReviewError(err error) protocol.FailureClass {
	var failure *provider.Error
	if errors.As(err, &failure) {
		return failure.Class
	}
	if errors.Is(err, context.Canceled) {
		return protocol.FailureCancelled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return protocol.FailureTimeout
	}
	return protocol.FailureProvider
}

func normalizeAttempt(attempt protocol.Attempt, number int, fallbackDurationMS int64) protocol.Attempt {
	attempt.Number = number
	if attempt.DurationMS < 0 || (attempt.DurationMS == 0 && fallbackDurationMS > 0) {
		attempt.DurationMS = fallbackDurationMS
	}
	return attempt
}

func malformedAttempt(number int, durationMS int64) protocol.Attempt {
	return protocol.Attempt{
		Number:     number,
		Outcome:    protocol.AttemptMalformed,
		DurationMS: durationMS,
		ErrorClass: classPointer(protocol.FailureProtocol),
	}
}

func classify(err error) protocol.FailureClass {
	var failure *provider.Error
	if errors.As(err, &failure) {
		return failure.Class
	}
	switch {
	case errors.Is(err, context.Canceled):
		return protocol.FailureCancelled
	case errors.Is(err, context.DeadlineExceeded):
		return protocol.FailureTimeout
	case errors.Is(err, target.ErrSecretFound):
		return protocol.FailureSecretScan
	case errors.Is(err, target.ErrSecretScan):
		return protocol.FailureSecretScan
	case errors.Is(err, target.ErrSourceChanged):
		return protocol.FailureSourceChanged
	default:
		return protocol.FailureTarget
	}
}

func elapsedMilliseconds(start, end time.Time) int64 {
	if end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}

func safeMessage(err error) string {
	message := "operation failed"
	if err != nil {
		message = strings.TrimSpace(err.Error())
	}
	if message == "" || !utf8.ValidString(message) {
		message = "operation failed"
	}
	message = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) && character != '\n' && character != '\t' {
			return ' '
		}
		return character
	}, message)
	runes := []rune(message)
	if len(runes) > 2000 {
		message = string(runes[:1997]) + "..."
	}
	return message
}

func classPointer(class protocol.FailureClass) *protocol.FailureClass {
	copy := class
	return &copy
}
