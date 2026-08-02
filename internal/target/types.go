package target

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/uinaf/autoreview/internal/protocol"
)

var (
	ErrSourceChanged = errors.New("reviewed source changed")
	ErrSecretFound   = errors.New("secret scan found credentials")
	ErrSecretScan    = errors.New("secret scan failed")
	ErrNoChanges     = errors.New("target has no changed files")
)

type Request struct {
	Mode         protocol.TargetMode
	Base         string
	Commit       string
	Prompt       string
	ContextFiles []string
	MaxBytes     int64
}

type Scanner interface {
	Scan(context.Context, []byte) error
}

type Options struct {
	GitPath        string
	TruffleHogPath string
	Scanner        Scanner
}

type Contributor struct {
	Name  string
	Bytes int64
}

type byteBudget struct {
	limit        int64
	used         int64
	framing      int64
	contributors []Contributor
}

func newByteBudget(limit int64) *byteBudget {
	return &byteBudget{limit: limit}
}

func (budget *byteBudget) Add(name string, size int64) {
	budget.Charge(size)
	budget.Track(name, size)
}

func (budget *byteBudget) Charge(size int64) {
	const maxInt64 = int64(^uint64(0) >> 1)
	if size > maxInt64-budget.used {
		budget.used = maxInt64
	} else {
		budget.used += size
	}
}

func (budget *byteBudget) Track(name string, size int64) {
	budget.contributors = append(budget.contributors, Contributor{Name: name, Bytes: size})
	sort.Slice(budget.contributors, func(i, j int) bool { return budget.contributors[i].Bytes > budget.contributors[j].Bytes })
	if len(budget.contributors) > 5 {
		budget.contributors = budget.contributors[:5]
	}
}

func (budget *byteBudget) AddFraming(size int64) {
	budget.Charge(size)
	budget.framing += size
	filtered := budget.contributors[:0]
	for _, contributor := range budget.contributors {
		if contributor.Name != "framing" {
			filtered = append(filtered, contributor)
		}
	}
	budget.contributors = filtered
}

func (budget *byteBudget) Read(root, path, contributor string) ([]byte, int64, error) {
	remaining := int64(-1)
	if budget.used <= budget.limit {
		remaining = budget.limit - budget.used
	}
	content, size, err := readContainedFile(root, path, remaining)
	if err != nil {
		return nil, 0, err
	}
	budget.Add(contributor, size)
	return content, size, nil
}

func (budget *byteBudget) Exceeded() bool {
	return budget.used > budget.limit
}

func (budget *byteBudget) Remaining() int64 {
	if budget.Exceeded() {
		return -1
	}
	return budget.limit - budget.used
}

func (budget *byteBudget) SizeError() error {
	if !budget.Exceeded() {
		return nil
	}
	contributors := append([]Contributor(nil), budget.contributors...)
	contributors = append(contributors, Contributor{Name: "framing", Bytes: budget.framing})
	return &SizeError{Limit: budget.limit, Actual: budget.used, Contributors: contributors}
}

type Bundle struct {
	repository   string
	request      Request
	collector    *Collector
	target       protocol.Target
	payload      []byte
	contributors []Contributor
}

func (bundle *Bundle) Payload() []byte {
	return append([]byte(nil), bundle.payload...)
}

func (bundle *Bundle) Repository() string {
	return bundle.repository
}

func (bundle *Bundle) Target() protocol.Target {
	target := bundle.target
	target.Files = append([]protocol.ReviewedFile(nil), bundle.target.Files...)
	for index := range target.Files {
		target.Files[index].LineRanges = append([]protocol.LineRange(nil), bundle.target.Files[index].LineRanges...)
	}
	return target
}

func (bundle *Bundle) Contributors() []Contributor {
	return append([]Contributor(nil), bundle.contributors...)
}

func (bundle *Bundle) VerifyUnchanged(ctx context.Context) error {
	current, err := bundle.collector.collect(ctx, bundle.repository, bundle.request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("%w: recollect target: %v", ErrSourceChanged, err)
	}
	if current.target.SnapshotHash != bundle.target.SnapshotHash {
		return ErrSourceChanged
	}
	return nil
}

type SizeError struct {
	Limit        int64
	Actual       int64
	Contributors []Contributor
}

func (sizeError *SizeError) Error() string {
	return fmt.Sprintf("bundle exceeds limit %d (observed at least %d bytes); largest contributors: %s", sizeError.Limit, sizeError.Actual, formatContributors(sizeError.Contributors))
}

func formatContributors(contributors []Contributor) string {
	contributors = append([]Contributor(nil), contributors...)
	sort.Slice(contributors, func(i, j int) bool {
		if contributors[i].Bytes == contributors[j].Bytes {
			return contributors[i].Name < contributors[j].Name
		}
		return contributors[i].Bytes > contributors[j].Bytes
	})
	if len(contributors) > 5 {
		contributors = contributors[:5]
	}
	result := ""
	for index, contributor := range contributors {
		if index > 0 {
			result += ", "
		}
		result += fmt.Sprintf("%s=%d", contributor.Name, contributor.Bytes)
	}
	return result
}
