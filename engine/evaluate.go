package engine

import (
	"fmt"
	"time"

	"github.com/google/cel-go/cel"
)

// Evaluate resolves the rules of a layer against a context at an instant.
//
// It applies the deterministic filters in order: validity (valid_from inclusive,
// valid_to exclusive), exact scope match, and the precompiled CEL condition. It
// never touches the network, disk or a database, and never recompiles CEL. The
// winner is still chosen provisionally by compiled ID order; specificity ranking
// (T2.3) and tie-breaking (T2.4) replace that choice, so the contract is provisional.
func (p *Program) Evaluate(input Evaluation) (Result, error) {
	result := Result{
		RuleSetID:      p.ruleSetID,
		RuleSetVersion: p.version,
		Layer:          input.Layer,
		Status:         ResultNoMatch,
		Trace:          EvaluationTrace{Candidates: []TraceEntry{}},
	}

	rules := p.layers[input.Layer]
	survivors := make([]compiledRule, 0, len(rules))
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
		survivors = append(survivors, candidate)
	}

	if len(survivors) == 0 {
		return result, nil
	}

	// Provisional selection: first survivor in compiled ID order. Replaced by
	// specificity ranking and tie-breaking in T2.3–T2.4.
	winner := survivors[0]
	result.Status = ResultMatch
	result.Winner = &ResolvedRule{RuleID: winner.rule.ID, Outcome: winner.rule.Outcome}
	result.Trace.Candidates = append(result.Trace.Candidates, TraceEntry{
		RuleID: winner.rule.ID,
		Status: TraceWinner,
	})
	for _, shadowed := range survivors[1:] {
		result.Trace.Candidates = append(result.Trace.Candidates, TraceEntry{
			RuleID: shadowed.rule.ID,
			Status: TraceShadowed,
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
