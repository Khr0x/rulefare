// Package engine compiles and resolves versioned rule sets without I/O or
// dependencies on the application hosting it.
package engine

import (
	"time"

	"github.com/google/cel-go/cel"
)

// ValueType is a CEL-compatible type available in an evaluation context.
type ValueType string

const (
	ValueBool      ValueType = "bool"
	ValueInt       ValueType = "int"
	ValueString    ValueType = "string"
	ValueTimestamp ValueType = "timestamp"
)

// Schema defines the variables available to CEL conditions.
type Schema struct {
	Version   string               `json:"version"`
	Variables map[string]ValueType `json:"variables"`
}

// RuleSet is an immutable, versioned collection after compilation.
type RuleSet struct {
	ID            string   `json:"id"`
	Version       string   `json:"version"`
	SchemaVersion string   `json:"schema_version"`
	Dimensions    []string `json:"dimensions"`
	Rules         []Rule   `json:"rules"`
}

// Composition controls how matching rules are resolved.
type Composition string

const CompositionFirstMatch Composition = "FIRST_MATCH"

// Rule describes when an outcome applies.
type Rule struct {
	ID          string            `json:"id"`
	Layer       string            `json:"layer"`
	Scope       map[string]string `json:"scope,omitempty"`
	Condition   string            `json:"condition,omitempty"`
	Composition Composition       `json:"composition,omitempty"`
	ValidFrom   *time.Time        `json:"valid_from,omitempty"`
	ValidTo     *time.Time        `json:"valid_to,omitempty"`
	Priority    int32             `json:"priority,omitempty"`
	Outcome     Outcome           `json:"outcome"`
}

// OutcomeKind identifies the single payload carried by an Outcome.
type OutcomeKind string

const (
	OutcomePercentage OutcomeKind = "percentage"
	OutcomeFixed      OutcomeKind = "fixed"
	OutcomeTiered     OutcomeKind = "tiered"
)

// Outcome is a discriminated union. Exactly one payload must match Kind.
type Outcome struct {
	Kind       OutcomeKind        `json:"kind"`
	Percentage *PercentageOutcome `json:"percentage,omitempty"`
	Fixed      *FixedOutcome      `json:"fixed,omitempty"`
	Tiered     *TieredOutcome     `json:"tiered,omitempty"`
}

// PercentageOutcome stores a ratio as a canonical decimal string.
type PercentageOutcome struct {
	Rate string `json:"rate"`
}

// FixedOutcome stores an amount without performing financial arithmetic.
type FixedOutcome struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// TieredOutcome selects rates later using the named integer metric.
type TieredOutcome struct {
	Metric string `json:"metric"`
	Tiers  []Tier `json:"tiers"`
}

// Tier applies until UpToExclusive; nil is the required unbounded last tier.
type Tier struct {
	UpToExclusive *int64 `json:"up_to_exclusive"`
	Rate          string `json:"rate"`
}

// Evaluation is the runtime input whose behavior is implemented in Week 2.
type Evaluation struct {
	Layer       string         `json:"layer"`
	EffectiveAt time.Time      `json:"effective_at"`
	Context     map[string]any `json:"context"`
}

// ResultStatus identifies whether evaluation found a winning rule.
type ResultStatus string

const (
	ResultMatch     ResultStatus = "MATCH"
	ResultNoMatch   ResultStatus = "NO_MATCH"
	ResultAmbiguous ResultStatus = "AMBIGUOUS_MATCH"
)

// TraceStatus explains how a candidate participated in resolution.
type TraceStatus string

const (
	TraceNotEffective   TraceStatus = "NOT_EFFECTIVE"
	TraceScopeMismatch  TraceStatus = "SCOPE_MISMATCH"
	TraceConditionFalse TraceStatus = "CONDITION_FALSE"
	TraceConditionError TraceStatus = "CONDITION_ERROR"
	TraceShadowed       TraceStatus = "SHADOWED"
	TraceWinner         TraceStatus = "WINNER"
	TraceAmbiguous      TraceStatus = "AMBIGUOUS"
)

// TraceEntry records one candidate without copying the evaluation context.
type TraceEntry struct {
	RuleID      string      `json:"rule_id"`
	Status      TraceStatus `json:"status"`
	Specificity []bool      `json:"specificity,omitempty"`
	Reason      string      `json:"reason,omitempty"`
}

// EvaluationTrace records deterministic candidate resolution.
type EvaluationTrace struct {
	Candidates  []TraceEntry `json:"candidates"`
	TieBreakers []string     `json:"tie_breakers,omitempty"`
}

// ResolvedRule is the winning rule and its validated outcome.
type ResolvedRule struct {
	RuleID  string  `json:"rule_id"`
	Outcome Outcome `json:"outcome"`
}

// Result is the stable runtime contract implemented in Week 2.
type Result struct {
	RuleSetID      string          `json:"ruleset_id"`
	RuleSetVersion string          `json:"ruleset_version"`
	Layer          string          `json:"layer"`
	Status         ResultStatus    `json:"status"`
	Winner         *ResolvedRule   `json:"winner,omitempty"`
	Trace          EvaluationTrace `json:"trace"`
}

// Severity classifies a validation issue.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// IssueCode is a stable, machine-readable validation code.
type IssueCode string

const (
	IssueRequired               IssueCode = "REQUIRED"
	IssueDuplicate              IssueCode = "DUPLICATE"
	IssueUnknownType            IssueCode = "UNKNOWN_TYPE"
	IssueUnknownDimension       IssueCode = "UNKNOWN_DIMENSION"
	IssueInvalidRange           IssueCode = "INVALID_RANGE"
	IssueInvalidCEL             IssueCode = "INVALID_CEL"
	IssueCELNotBool             IssueCode = "CEL_NOT_BOOL"
	IssueInvalidOutcome         IssueCode = "INVALID_OUTCOME"
	IssueLimitExceeded          IssueCode = "LIMIT_EXCEEDED"
	IssueUnsupportedComposition IssueCode = "UNSUPPORTED_COMPOSITION"
	IssueSchemaMismatch         IssueCode = "SCHEMA_MISMATCH"
)

// ValidationIssue points to invalid input with an RFC 6901-style JSON path.
type ValidationIssue struct {
	Code     IssueCode `json:"code"`
	Path     string    `json:"path"`
	Message  string    `json:"message"`
	Severity Severity  `json:"severity"`
}

// ValidationReport contains all safely discoverable issues.
type ValidationReport struct {
	Issues []ValidationIssue `json:"issues"`
}

// Valid reports whether compilation may produce a Program.
func (r ValidationReport) Valid() bool {
	for _, issue := range r.Issues {
		if issue.Severity == SeverityError {
			return false
		}
	}
	return true
}

// Program is immutable after Compile returns it.
type Program struct {
	schema        Schema
	ruleSetID     string
	version       string
	schemaVersion string
	dimensions    []string
	layers        map[string][]compiledRule
}

type compiledRule struct {
	rule    Rule
	program cel.Program
}
