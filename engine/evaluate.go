package engine

import (
	"fmt"
	"sort"
	"time"

	"github.com/google/cel-go/cel"
)

// Evaluate resolves the rules of a layer against a context at an instant.
//
// It applies the deterministic filters in order: validity (valid_from inclusive,
// valid_to exclusive), exact scope match, and the precompiled CEL condition. It
// never touches the network, disk or a database, and never recompiles CEL.
//
// Surviving rules are ranked by specificity (the lexicographic scope profile
// over Dimensions, most specific dimension first) and, within an identical
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

	rules := p.layers[input.Layer]
	survivors := make([]rankedRule, 0, len(rules))
	for _, candidate := range rules {
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
		matched, condErr := evalCondition(candidate.program, input.Context)
		if condErr != nil {
			result.Trace.Candidates = append(result.Trace.Candidates, TraceEntry{
				RuleID: candidate.rule.ID,
				Status: TraceConditionError,
				Reason: condErr.Error(),
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
			rule:        candidate.rule,
			specificity: dimensionSpecificity(candidate.rule.Scope, p.dimensions),
		})
	}

	if len(survivors) == 0 {
		return result, nil
	}

	// Order by specificity (descending), then Priority (descending). Id only
	// stabilises the presentation of otherwise-equal candidates; it never
	// decides the winner.
	sort.SliceStable(survivors, func(i, j int) bool {
		if c := compareSpecificity(survivors[i].specificity, survivors[j].specificity); c != 0 {
			return c > 0
		}
		if survivors[i].rule.Priority != survivors[j].rule.Priority {
			return survivors[i].rule.Priority > survivors[j].rule.Priority
		}
		return survivors[i].rule.ID < survivors[j].rule.ID
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
// the assertion is defensive. A runtime failure (missing or wrong-typed
// variable) is returned as an error and classified as CONDITION_ERROR upstream.
func evalCondition(prg cel.Program, ctx map[string]any) (bool, error) {
	out, _, err := prg.Eval(ctx)
	if err != nil {
		return false, err
	}
	matched, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("condition did not evaluate to bool")
	}
	return matched, nil
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
