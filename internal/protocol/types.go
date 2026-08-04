package protocol

const SchemaVersion = "1"

const (
	MaxLineNumber     = 1<<31 - 1
	MaxAttemptNumber  = 1<<31 - 1
	MaxPathCharacters = 4096
)

type Status string

const (
	StatusClean    Status = "clean"
	StatusFindings Status = "findings"
	StatusFailure  Status = "failure"
)

type Priority string

const (
	PriorityP0 Priority = "P0"
	PriorityP1 Priority = "P1"
	PriorityP2 Priority = "P2"
	PriorityP3 Priority = "P3"
)

type Category string

const (
	CategoryBug             Category = "bug"
	CategorySecurity        Category = "security"
	CategoryRegression      Category = "regression"
	CategoryTestGap         Category = "test_gap"
	CategoryMaintainability Category = "maintainability"
)

type ProviderName string

const (
	ProviderCodex  ProviderName = "codex"
	ProviderClaude ProviderName = "claude"
	ProviderCursor ProviderName = "cursor"
	ProviderGrok   ProviderName = "grok"
)

type Isolation string

const (
	IsolationStrict Isolation = "strict"
	IsolationNative Isolation = "native"
)

type TargetMode string

const (
	TargetLocal  TargetMode = "local"
	TargetBranch TargetMode = "branch"
	TargetCommit TargetMode = "commit"
)

type AttemptOutcome string

const (
	AttemptValid     AttemptOutcome = "valid"
	AttemptMalformed AttemptOutcome = "malformed"
	AttemptFailed    AttemptOutcome = "failed"
)

type FailureClass string

const (
	FailureConfig        FailureClass = "config"
	FailureTarget        FailureClass = "target"
	FailureSecretScan    FailureClass = "secret_scan"
	FailureCapability    FailureClass = "capability"
	FailureAuth          FailureClass = "authentication"
	FailureTimeout       FailureClass = "timeout"
	FailureCancelled     FailureClass = "cancelled"
	FailureProvider      FailureClass = "provider"
	FailureProtocol      FailureClass = "protocol"
	FailureSourceChanged FailureClass = "source_changed"
	FailureInternal      FailureClass = "internal"
)

type RecoveryStrategy string

const (
	RecoveryCursorTrailingObject RecoveryStrategy = "cursor_trailing_object"
)

type Report struct {
	SchemaVersion string   `json:"schema_version"`
	Status        Status   `json:"status"`
	Review        *Review  `json:"review"`
	Failure       *Failure `json:"failure"`
	Metadata      Metadata `json:"metadata"`
}

type Review struct {
	Findings           []Finding `json:"findings"`
	OverallExplanation string    `json:"overall_explanation"`
	OverallConfidence  float64   `json:"overall_confidence"`
}

type Finding struct {
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	Priority   Priority `json:"priority"`
	Confidence float64  `json:"confidence"`
	Category   Category `json:"category"`
	Location   Location `json:"location"`
}

type Location struct {
	FilePath  string `json:"file_path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type Failure struct {
	Class   FailureClass `json:"class"`
	Message string       `json:"message"`
}

type Metadata struct {
	Target           *Target          `json:"target"`
	Provider         *Provider        `json:"provider"`
	Attempts         []Attempt        `json:"attempts"`
	DurationMS       int64            `json:"duration_ms"`
	Isolation        *Isolation       `json:"isolation"`
	WebAccess        bool             `json:"web_access"`
	ProtocolRecovery ProtocolRecovery `json:"protocol_recovery"`
}

type Target struct {
	Mode           TargetMode     `json:"mode"`
	SnapshotHash   string         `json:"snapshot_hash"`
	HeadRevision   string         `json:"head_revision"`
	BaseRevision   string         `json:"base_revision"`
	CommitRevision string         `json:"commit_revision"`
	Files          []ReviewedFile `json:"files"`
}

type ReviewedFile struct {
	FilePath   string      `json:"file_path"`
	LineRanges []LineRange `json:"line_ranges"`
}

type LineRange struct {
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
}

type Provider struct {
	Name    ProviderName `json:"name"`
	Model   string       `json:"model"`
	Version string       `json:"version"`
}

type Attempt struct {
	Number     int            `json:"number"`
	Outcome    AttemptOutcome `json:"outcome"`
	DurationMS int64          `json:"duration_ms"`
	ErrorClass *FailureClass  `json:"error_class"`
}

type ProtocolRecovery struct {
	Applied  bool              `json:"applied"`
	Strategy *RecoveryStrategy `json:"strategy"`
}
