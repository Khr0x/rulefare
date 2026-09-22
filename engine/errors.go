package engine

import (
	"fmt"
	"strings"
)

// InvalidEvaluationError reports invalid runtime input before any rule runs.
// Report uses stable codes and JSON paths and never includes context values.
type InvalidEvaluationError struct {
	Report ValidationReport
}

func (e *InvalidEvaluationError) Error() string {
	return "invalid evaluation input"
}

// AmbiguousMatchError is returned by Evaluate when two or more surviving rules
// tie on both specificity and priority, so no deterministic winner exists. It
// is a blocking authoring error: the ruleset must disambiguate the listed
// rules. Resolution never falls back to load order, map order or rule id, so a
// persistent tie fails closed rather than picking an arbitrary rule.
type AmbiguousMatchError struct {
	Layer   string
	RuleIDs []string
}

func (e *AmbiguousMatchError) Error() string {
	return fmt.Sprintf("ambiguous match in layer %q between rules %s", e.Layer, strings.Join(e.RuleIDs, ", "))
}

// EvaluationLimitError aborts resolution when a CEL resource budget is exhausted.
// No winner or partial trace is returned; RuleID identifies the last attempted
// condition, without exposing context values or CEL error text.
type EvaluationLimitError struct{ RuleID string }

func (e *EvaluationLimitError) Error() string { return "evaluation resource limit exceeded" }
