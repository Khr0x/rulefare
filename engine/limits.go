package engine

import (
	"encoding/json"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/interpreter"
)

// Fixed v1 resource budgets. Text budgets count decoded bytes, not JSON syntax
// or Go object overhead. Cardinality limits bound structural allocations.
const (
	maxStringBytes             = 4096
	maxRegexBytes              = 256
	maxNameBytes               = 64
	maxMetadataBytes           = 128
	maxContextBytes            = 64 << 10
	maxCompilationBytes        = 8 << 20
	maxDecimalDigits           = 38
	maxDecimalScale            = 18
	maxConditionCost    uint64 = 10_000
	maxEvaluationCost   uint64 = 1_000_000
)

// Check lengths before sorting map keys, constructing error paths, parsing
// numbers, or invoking CEL. Do not traverse unsupported context containers.
func evaluationWithinLimits(input Evaluation) bool {
	if len(input.Layer) > maxMetadataBytes || len(input.Context) > maxVariables {
		return false
	}
	total := len(input.Layer)
	for name, value := range input.Context {
		if len(name) > maxNameBytes {
			return false
		}
		total += len(name)
		switch v := value.(type) {
		case string:
			if len(v) > maxStringBytes {
				return false
			}
			total += len(v)
		case json.Number:
			if len(v) > 20 {
				return false
			} // signed int64 literal
			total += len(v)
		}
		if total > maxContextBytes {
			return false
		}
	}
	return true
}

// A bounded preflight protects callers of Compile, including callers which do
// not use the CLI's JSON size limit. A single stable issue avoids reflecting
// attacker-controlled oversized map keys into diagnostics.
func compilationWithinLimits(schema Schema, set RuleSet) bool {
	if len(schema.Variables) > maxVariables || len(set.Rules) > maxRules || len(set.Dimensions) > maxDimensions {
		return false
	}
	remaining := maxCompilationBytes
	text := func(value string, limit int) bool {
		if len(value) > limit || len(value) > remaining {
			return false
		}
		remaining -= len(value)
		return true
	}
	if !text(schema.Version, maxMetadataBytes) || !text(set.ID, maxMetadataBytes) || !text(set.Version, maxMetadataBytes) || !text(set.SchemaVersion, maxMetadataBytes) {
		return false
	}
	for name, kind := range schema.Variables {
		if !text(name, maxNameBytes) || !text(string(kind), maxNameBytes) {
			return false
		}
	}
	for _, dimension := range set.Dimensions {
		if !text(dimension, maxNameBytes) {
			return false
		}
	}
	for _, rule := range set.Rules {
		if len(rule.Scope) > maxDimensions {
			return false
		}
		if !text(rule.ID, maxMetadataBytes) || !text(rule.Layer, maxMetadataBytes) || !text(string(rule.Composition), maxMetadataBytes) || !text(rule.Condition, maxConditionBytes) || !text(string(rule.Outcome.Kind), maxNameBytes) {
			return false
		}
		for name, value := range rule.Scope {
			if !text(name, maxNameBytes) || !text(value, maxStringBytes) {
				return false
			}
		}
		if p := rule.Outcome.Percentage; p != nil {
			if !text(p.Rate, maxDecimalDigits+1) {
				return false
			}
		}
		if f := rule.Outcome.Fixed; f != nil {
			if !text(f.Amount, maxDecimalDigits+1) || !text(f.Currency, 3) {
				return false
			}
		}
		if t := rule.Outcome.Tiered; t != nil {
			if len(t.Tiers) > maxTiers || !text(t.Metric, maxNameBytes) {
				return false
			}
			for _, tier := range t.Tiers {
				if !text(tier.Rate, maxDecimalDigits+1) {
					return false
				}
			}
		}
	}
	return true
}

// CEL charges regex cost after the match. Bound the pattern before entering
// RE2, including patterns computed by CEL rather than supplied in Context.
// Cancellation must unwind evaluation: returning a CEL error could be masked
// by a surrounding `|| true` and accidentally admit a fallback or winner.
func boundedRegexMatch(value, pattern ref.Val) ref.Val {
	if p, ok := pattern.(types.String); ok && len(p) > maxRegexBytes {
		panic(interpreter.EvalCancelledError{Cause: interpreter.CostLimitExceeded, Message: "regex pattern exceeds resource limit"})
	}
	return value.(traits.Matcher).Match(pattern)
}
