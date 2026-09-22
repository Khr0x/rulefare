package engine

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/interpreter"
)

// Evaluate resolves the rules of a layer against a context at an instant.
//
// It validates and normalizes the entire input before examining any rule. An
// InvalidEvaluationError returns INVALID_INPUT with no winner or candidates.
// Resource exhaustion returns LIMIT_EXCEEDED with EvaluationLimitError and
// no winner or partial trace. Cost is tracked per condition and evaluation.
// It then applies the deterministic filters in order: validity (valid_from inclusive,
// valid_to exclusive), exact scope match, and the precompiled CEL condition. It
// never touches the network, disk or a database, and never recompiles CEL.
//
// Surviving rules are ranked by specificity (the lexicographic scope profile
// over Dimensions, compared from the last dimension to the first) and, within an identical
// profile, by Priority. A rule that ties another on both is undecidable and
// yields an AmbiguousMatchError; resolution never falls back to load order, map
// order or id. The winning outcome is deep-copied so mutating the Result cannot
// affect the Program or a later evaluation.
func (p *Program) Evaluate(input Evaluation) (Result, error) {
	result := Result{
		RuleSetID:      p.ruleSetID,
		RuleSetVersion: p.version,
		Layer:          input.Layer,
		Status:         ResultNoMatch,
		Trace:          EvaluationTrace{Candidates: []TraceEntry{}},
	}

	if len(input.Layer) > maxMetadataBytes {
		result.Layer = ""
	}
	normalized, err := p.normalizeEvaluation(input)
	if err != nil {
		result.Status = ResultInvalidInput
		return result, err
	}
	input = normalized

	rules := p.layers[input.Layer]
	result.Trace.Candidates = make([]TraceEntry, 0, len(rules))
	survivors := make([]rankedRule, 0, len(rules))
	var cost uint64
	// CEL uses only immutable scalar inputs and pure bindings. Reuse successful
	// condition results within this call, but charge their cost for every rule.
	type conditionResult struct {
		matched bool
		cost    uint64
	}
	var conditions map[string]conditionResult
	for i := range rules {
		candidate := &rules[i]
		if !effective(candidate.rule, input.EffectiveAt) {
			result.Trace.Candidates = append(result.Trace.Candidates, TraceEntry{
				RuleID: candidate.rule.ID,
				Status: TraceNotEffective,
			})
			continue
		}
		if ok, mismatch := scopeMatch(candidate.rule.Scope, input.Context); !ok {
			result.Trace.Candidates = append(result.Trace.Candidates, TraceEntry{
				RuleID: candidate.rule.ID,
				Status: TraceScopeMismatch,
				Reason: mismatch,
			})
			continue
		}
		var matched bool
		var used uint64
		var condErr error
		cached, found := conditions[candidate.rule.Condition]
		if candidate.reuseCondition && found {
			matched, used = cached.matched, cached.cost
		} else {
			matched, used, condErr = evalCondition(candidate.program, input.Context)
			if candidate.reuseCondition && condErr == nil {
				if conditions == nil {
					conditions = make(map[string]conditionResult)
				}
				conditions[candidate.rule.Condition] = conditionResult{matched, used}
			}
		}
		if isCostLimit(condErr) || used > maxEvaluationCost-cost {
			result.Status = ResultLimitExceeded
			result.Trace = EvaluationTrace{Candidates: []TraceEntry{}}
			return result, &EvaluationLimitError{RuleID: candidate.rule.ID}
		}
		cost += used
		if condErr != nil {
			// CEL errors can embed context values. The stable status is the
			// public reason; never copy the internal error into the trace.
			result.Trace.Candidates = append(result.Trace.Candidates, TraceEntry{
				RuleID: candidate.rule.ID,
				Status: TraceConditionError,
			})
			continue
		}
		if !matched {
			result.Trace.Candidates = append(result.Trace.Candidates, TraceEntry{
				RuleID: candidate.rule.ID,
				Status: TraceConditionFalse,
			})
			continue
		}
		survivors = append(survivors, rankedRule{
			rule:        &candidate.rule,
			specificity: dimensionSpecificity(candidate.rule.Scope, p.dimensions),
		})
	}

	if len(survivors) == 0 {
		return result, nil
	}

	// Order by specificity (descending), then Priority (descending). Id only
	// stabilises the presentation of otherwise-equal candidates; it never
	// decides the winner.
	slices.SortFunc(survivors, func(a, b rankedRule) int {
		if c := compareSpecificity(a.specificity, b.specificity); c != 0 {
			return -c
		}
		if c := cmp.Compare(b.rule.Priority, a.rule.Priority); c != 0 {
			return c
		}
		return cmp.Compare(a.rule.ID, b.rule.ID)
	})

	top := survivors[0]

	// Rules sharing the top specificity are contiguous at the front; their tie
	// was arbitrated by priority, so record them as the tie-break set.
	specTied := 1
	for _, other := range survivors[1:] {
		if compareSpecificity(top.specificity, other.specificity) != 0 {
			break
		}
		specTied++
	}
	if specTied > 1 {
		result.Trace.TieBreakers = ruleIDs(survivors[:specTied])
	}

	// Among those, rules also sharing the top priority are undecidable.
	priorityTied := 1
	for _, other := range survivors[1:specTied] {
		if other.rule.Priority != top.rule.Priority {
			break
		}
		priorityTied++
	}
	if priorityTied > 1 {
		for _, ambiguous := range survivors[:priorityTied] {
			result.Trace.Candidates = append(result.Trace.Candidates, TraceEntry{
				RuleID:      ambiguous.rule.ID,
				Status:      TraceAmbiguous,
				Specificity: ambiguous.specificity,
			})
		}
		for _, shadowed := range survivors[priorityTied:] {
			result.Trace.Candidates = append(result.Trace.Candidates, TraceEntry{
				RuleID:      shadowed.rule.ID,
				Status:      TraceShadowed,
				Specificity: shadowed.specificity,
			})
		}
		result.Status = ResultAmbiguous
		return result, &AmbiguousMatchError{Layer: input.Layer, RuleIDs: ruleIDs(survivors[:priorityTied])}
	}

	result.Status = ResultMatch
	result.Winner = &ResolvedRule{
		RuleID:  top.rule.ID,
		Outcome: cloneOutcome(top.rule.Outcome),
	}
	result.Trace.Candidates = append(result.Trace.Candidates, TraceEntry{
		RuleID:      top.rule.ID,
		Status:      TraceWinner,
		Specificity: top.specificity,
	})
	for _, shadowed := range survivors[1:] {
		result.Trace.Candidates = append(result.Trace.Candidates, TraceEntry{
			RuleID:      shadowed.rule.ID,
			Status:      TraceShadowed,
			Specificity: shadowed.specificity,
		})
	}
	return result, nil
}

// Keep error inspection off the successful path: errors.As needs an address
// which otherwise escapes once for every evaluated condition.
func isCostLimit(err error) bool {
	if err == nil {
		return false
	}
	var cancelled interpreter.EvalCancelledError
	return errors.As(err, &cancelled) && cancelled.Cause == interpreter.CostLimitExceeded
}

// effective reports whether a rule is in force at the given instant. valid_from
// is inclusive and valid_to is exclusive; a nil bound is unbounded on that side.
func effective(rule Rule, at time.Time) bool {
	if rule.ValidFrom != nil && at.Before(*rule.ValidFrom) {
		return false
	}
	if rule.ValidTo != nil && !at.Before(*rule.ValidTo) {
		return false
	}
	return true
}

// evalCondition runs a precompiled CEL program against the context on the hot
// path; it never recompiles. Compile already guaranteed a bool output type, so
// the assertion is defensive. A runtime failure (such as an invalid conversion
// or division by zero) is returned internally and exposed only as the stable
// CONDITION_ERROR status upstream, never as raw error text.
func evalCondition(prg cel.Program, ctx map[string]any) (bool, uint64, error) {
	out, details, err := prg.Eval(ctx)
	var cost uint64
	if details != nil && details.ActualCost() != nil {
		cost = *details.ActualCost()
	}
	if err != nil {
		return false, cost, err
	}
	matched, ok := out.Value().(bool)
	if !ok {
		return false, cost, fmt.Errorf("condition did not evaluate to bool")
	}
	return matched, cost, nil
}

// scopeMatch reports whether every scoped dimension equals its context value.
// An empty scope is a global rule and always matches. On mismatch it returns
// the first offending dimension in sorted order for a stable trace reason. A
// dimension absent from the context or holding a non-string value is a mismatch.
func scopeMatch(scope map[string]string, ctx map[string]any) (bool, string) {
	for _, dimension := range sortedKeys(scope) {
		raw, present := ctx[dimension]
		if !present {
			return false, dimension
		}
		value, ok := raw.(string)
		if !ok || value != scope[dimension] {
			return false, dimension
		}
	}
	return true, ""
}
