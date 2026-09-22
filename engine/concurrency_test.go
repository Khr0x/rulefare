package engine

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEvaluateConcurrent(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	rules := loadFixture[RuleSet](t, "ruleset.json")
	for _, id := range []string{"TIE_A", "TIE_B"} {
		rule := pctRule(id, 0, map[string]string{"country": "MX"})
		rule.Layer = "ambiguous"
		rules.Rules = append(rules.Rules, rule)
	}
	broken := pctRule("CONVERSION", 0, nil)
	broken.Layer, broken.Condition = "error", "int(country) > 0"
	rules.Rules = append(rules.Rules, broken)
	limited := pctRule("LIMITED", 0, nil)
	limited.Layer, limited.Condition = "limit", "country.contains(supplier)"
	rules.Rules = append(rules.Rules, limited)

	at := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	context := fixtureContext(map[string]any{
		"country": "MX", "supplier": "hotelbeds", "contract": "ctr_hb_2026",
		"active": true, "amount_minor": json.Number("200000"),
		"travel_date": "2026-07-01T00:00:00Z",
	})
	cases := []struct {
		name   string
		input  Evaluation
		status ResultStatus
		winner string
	}{
		{"percentage", Evaluation{Layer: "operation", EffectiveAt: at, Context: context}, ResultMatch, "HOTELBEDS_MX"},
		{"fixed", Evaluation{Layer: "operation", EffectiveAt: at, Context: fixtureContext(map[string]any{"country": "US", "supplier": "hotelbeds", "active": true})}, ResultMatch, "HOTELBEDS_FIXED"},
		{"tiered", Evaluation{Layer: "split", EffectiveAt: at, Context: context}, ResultMatch, "CONTRACT_TIERS"},
		{"no_match", Evaluation{Layer: "split", EffectiveAt: at, Context: fixtureContext(nil)}, ResultNoMatch, ""},
		{"ambiguous", Evaluation{Layer: "ambiguous", EffectiveAt: at, Context: context}, ResultAmbiguous, ""},
		{"invalid_context", Evaluation{Layer: "operation", EffectiveAt: at, Context: fixtureContext(map[string]any{"active": "invalid"})}, ResultInvalidInput, ""},
		{"invalid_layer", Evaluation{Layer: "missing", EffectiveAt: at, Context: context}, ResultInvalidInput, ""},
		{"cost_limit", Evaluation{Layer: "limit", EffectiveAt: at, Context: fixtureContext(map[string]any{"country": strings.Repeat("a", maxStringBytes), "supplier": strings.Repeat("a", maxStringBytes)})}, ResultLimitExceeded, ""},
		{"condition_error", Evaluation{Layer: "error", EffectiveAt: at, Context: context}, ResultNoMatch, ""},
	}

	// Use a separate reference Program so the shared Program's first evaluation
	// also happens concurrently, without warming any CEL program beforehand.
	reference, report := Compile(schema, rules)
	if reference == nil {
		t.Fatal(report)
	}
	wantJSON := make([][]byte, len(cases))
	wantErrors := make([]error, len(cases))
	for i, tc := range cases {
		result, err := reference.Evaluate(tc.input)
		if result.Status != tc.status || (tc.winner == "" && result.Winner != nil) ||
			(tc.winner != "" && (result.Winner == nil || result.Winner.RuleID != tc.winner)) {
			t.Fatalf("%s: invalid reference result: %+v", tc.name, result)
		}
		switch tc.status {
		case ResultAmbiguous:
			if _, ok := err.(*AmbiguousMatchError); !ok {
				t.Fatalf("%s: expected ambiguity error, got %v", tc.name, err)
			}
		case ResultLimitExceeded:
			if _, ok := err.(*EvaluationLimitError); !ok {
				t.Fatalf("%s: expected limit error: %v", tc.name, err)
			}
		case ResultInvalidInput:
			if _, ok := err.(*InvalidEvaluationError); !ok {
				t.Fatalf("%s: expected input error, got %v", tc.name, err)
			}
		default:
			if err != nil {
				t.Fatal(err)
			}
		}
		if tc.name == "condition_error" && (len(result.Trace.Candidates) != 1 || result.Trace.Candidates[0].Status != TraceConditionError) {
			t.Fatalf("condition error was not exercised: %+v", result.Trace)
		}
		wantErrors[i] = err
		wantJSON[i], err = json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
	}

	for _, mode := range []string{"read_only", "mutate_returned_values"} {
		t.Run(mode, func(t *testing.T) {
			program, report := Compile(schema, rules)
			if program == nil {
				t.Fatal(report)
			}
			check := func(index int) {
				t.Helper()
				tc := cases[index]
				result, evalErr := program.Evaluate(tc.input)
				encoded, err := json.Marshal(result)
				if err != nil || !bytes.Equal(encoded, wantJSON[index]) || !reflect.DeepEqual(evalErr, wantErrors[index]) {
					t.Errorf("%s: concurrent evaluation changed: result %s, error %v, marshal error %v", tc.name, encoded, evalErr, err)
					return
				}
				if mode == "mutate_returned_values" {
					mutateEvaluationOutput(&result, evalErr)
				}
			}

			const workers, iterations = 16, 64
			start := make(chan struct{})
			var wg sync.WaitGroup
			for worker := 0; worker < workers; worker++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					for iteration := 0; iteration < iterations; iteration++ {
						check((worker + iteration) % len(cases))
					}
				}()
			}
			close(start)
			wg.Wait()
			// Catch mutations from the final iteration even after writers stop.
			for i := range cases {
				check(i)
			}
		})
	}
}

// Mutate only caller-owned outputs, never the contexts shared by evaluators.
func mutateEvaluationOutput(result *Result, err error) {
	if result.Winner != nil {
		result.Winner.RuleID = "changed"
		outcome := &result.Winner.Outcome
		if outcome.Percentage != nil {
			outcome.Percentage.Rate = "0.99"
		}
		if outcome.Fixed != nil {
			outcome.Fixed.Amount, outcome.Fixed.Currency = "999", "USD"
		}
		if outcome.Tiered != nil {
			outcome.Tiered.Metric = "changed"
			for i := range outcome.Tiered.Tiers {
				tier := &outcome.Tiered.Tiers[i]
				tier.Rate = "0.99"
				if tier.UpToExclusive != nil {
					*tier.UpToExclusive = -1
				}
			}
		}
	}
	for i := range result.Trace.Candidates {
		entry := &result.Trace.Candidates[i]
		entry.RuleID, entry.Reason, entry.Status = "changed", "changed", TraceWinner
		for j := range entry.Specificity {
			entry.Specificity[j] = !entry.Specificity[j]
		}
	}
	for i := range result.Trace.TieBreakers {
		result.Trace.TieBreakers[i] = "changed"
	}
	switch e := err.(type) {
	case *AmbiguousMatchError:
		e.Layer = "changed"
		for i := range e.RuleIDs {
			e.RuleIDs[i] = "changed"
		}
	case *EvaluationLimitError:
		e.RuleID = "changed"
	case *InvalidEvaluationError:
		for i := range e.Report.Issues {
			e.Report.Issues[i].Path = "changed"
		}
	}
}
