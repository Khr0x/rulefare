package engine

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
)

func TestEvaluationSizeLimits(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	program, report := Compile(schema, minimalRuleSet("true"))
	if program == nil {
		t.Fatal(report)
	}
	for _, tc := range []struct {
		name   string
		change func(*Evaluation)
	}{
		{"string", func(e *Evaluation) { e.Context["country"] = strings.Repeat("x", maxStringBytes+1) }},
		{"multibyte string", func(e *Evaluation) { e.Context["country"] = strings.Repeat("é", maxStringBytes/2+1) }},
		{"number", func(e *Evaluation) { e.Context["amount_minor"] = json.Number(strings.Repeat("9", 1<<20)) }},
		{"key", func(e *Evaluation) { e.Context[strings.Repeat("/", 1<<20)] = true }},
		{"cardinality", func(e *Evaluation) {
			for i := 0; i <= maxVariables; i++ {
				e.Context[fmt.Sprint(i)] = true
			}
		}},
		{"layer", func(e *Evaluation) { e.Layer = strings.Repeat("x", maxMetadataBytes+1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := benchmarkEvaluation()
			tc.change(&input)
			result, err := program.Evaluate(input)
			invalid, ok := err.(*InvalidEvaluationError)
			if !ok || result.Status != ResultInvalidInput || result.Winner != nil || len(result.Trace.Candidates) != 0 {
				t.Fatalf("limit activated fallback: %+v, %v", result, err)
			}
			if len(invalid.Report.Issues) != 1 || invalid.Report.Issues[0].Code != IssueLimitExceeded {
				t.Fatal(invalid.Report)
			}
			encoded, _ := json.Marshal(result)
			if len(encoded) > 1024 {
				t.Fatal("oversized input reflected in result")
			}
		})
	}
	input := benchmarkEvaluation()
	input.Context["country"] = strings.Repeat("x", maxStringBytes)
	if result, err := program.Evaluate(input); err != nil || result.Status != ResultMatch {
		t.Fatalf("boundary rejected: %+v %v", result, err)
	}
}

func TestContextTotalBoundary(t *testing.T) {
	schema := Schema{Version: "v1", Variables: map[string]ValueType{}}
	rules := minimalRuleSet("true")
	rules.Dimensions = nil
	input := benchmarkEvaluation()
	input.Context = map[string]any{}
	remaining := maxContextBytes - len(input.Layer) - 16*3
	for i := 0; i < 16; i++ {
		name := fmt.Sprintf("v%02d", i)
		schema.Variables[name] = ValueString
		n := min(remaining, maxStringBytes)
		input.Context[name] = strings.Repeat("x", n)
		remaining -= n
	}
	program, report := Compile(schema, rules)
	if program == nil {
		t.Fatal(report)
	}
	if _, err := program.Evaluate(input); err != nil {
		t.Fatal(err)
	}
	input.Context["v15"] = input.Context["v15"].(string) + "x"
	if result, err := program.Evaluate(input); err == nil || result.Status != ResultInvalidInput {
		t.Fatal("aggregate byte budget not enforced")
	}
}

func TestCompilationResourceLimits(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	for _, tc := range []struct {
		name   string
		change func(*Schema, *RuleSet)
	}{
		{"rules", func(_ *Schema, r *RuleSet) { r.Rules = make([]Rule, maxRules+1) }},
		{"variables", func(s *Schema, _ *RuleSet) {
			for i := 0; i <= maxVariables; i++ {
				s.Variables[fmt.Sprintf("v%d", i)] = ValueString
			}
		}},
		{"dimensions", func(_ *Schema, r *RuleSet) { r.Dimensions = make([]string, maxDimensions+1) }},
		{"scope", func(_ *Schema, r *RuleSet) {
			r.Rules[0].Scope = map[string]string{}
			for i := 0; i <= maxDimensions; i++ {
				r.Rules[0].Scope[fmt.Sprint(i)] = "x"
			}
		}},
		{"scope value", func(_ *Schema, r *RuleSet) {
			r.Rules[0].Scope = map[string]string{"country": strings.Repeat("x", maxStringBytes+1)}
		}},
		{"tiers", func(_ *Schema, r *RuleSet) {
			r.Rules[0].Outcome = Outcome{Kind: OutcomeTiered, Tiered: &TieredOutcome{Tiers: make([]Tier, maxTiers+1)}}
		}},
		{"decimal", func(_ *Schema, r *RuleSet) { r.Rules[0].Outcome.Percentage.Rate = strings.Repeat("9", 1<<20) }},
		{"metadata", func(s *Schema, _ *RuleSet) { s.Version = strings.Repeat("x", 1<<20) }},
		{"map key", func(s *Schema, _ *RuleSet) { s.Variables[strings.Repeat("/", 1<<20)] = ValueString }},
		{"total", func(_ *Schema, r *RuleSet) {
			*r = benchmarkRules(2100)
			for i := range r.Rules {
				r.Rules[i].Scope["country"] = strings.Repeat("x", maxStringBytes)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := cloneSchema(schema)
			r := minimalRuleSet("true")
			tc.change(&s, &r)
			program, report := Compile(s, r)
			if program != nil || len(report.Issues) != 1 || report.Issues[0].Code != IssueLimitExceeded {
				t.Fatalf("unbounded input: %+v", report)
			}
		})
	}
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{strings.Repeat("9", 38), true}, {strings.Repeat("9", 39), false},
		{"0." + strings.Repeat("1", 18), true}, {"0." + strings.Repeat("1", 19), false},
		{strings.Repeat("9", 20) + "." + strings.Repeat("1", 18), true},
		{strings.Repeat("9", 21) + "." + strings.Repeat("1", 18), false},
	} {
		if canonicalDecimal(tc.value) != tc.valid {
			t.Fatalf("decimal boundary: %s", tc.value)
		}
	}
}

func TestCELCostLimitsFailClosed(t *testing.T) {
	for _, aggregate := range []bool{false, true} {
		t.Run(fmt.Sprint(aggregate), func(t *testing.T) {
			rules := minimalRuleSet("country.contains(supplier)")
			count := 1
			if aggregate {
				count = 3000
			}
			rules.Rules = make([]Rule, count+1)
			for i := 0; i < count; i++ {
				rule := pctRule(fmt.Sprintf("R%05d", i), int32(i+1), nil)
				rule.Condition = "country.contains(supplier)"
				rules.Rules[i] = rule
			}
			rules.Rules[count] = pctRule("A_FALLBACK", 0, nil) // evaluated before costly rules
			program, report := Compile(loadFixture[Schema](t, "schema.json"), rules)
			if program == nil {
				t.Fatal(report)
			}
			input := benchmarkEvaluation()
			input.Context["country"] = strings.Repeat("a", maxStringBytes)
			if aggregate {
				input.Context["supplier"] = "a"
			} else {
				input.Context["supplier"] = strings.Repeat("a", maxStringBytes)
			}
			for i := 0; i < 2; i++ {
				result, err := program.Evaluate(input)
				limit, ok := err.(*EvaluationLimitError)
				if !ok || limit.RuleID == "" || result.Status != ResultLimitExceeded || result.Winner != nil || len(result.Trace.Candidates) != 0 {
					t.Fatalf("cost limit activated fallback: %+v %v", result, err)
				}
			}
			// A failed call cannot poison the next evaluation of a shared program.
			if result, err := program.Evaluate(benchmarkEvaluation()); err != nil || result.Winner == nil || result.Winner.RuleID != "A_FALLBACK" {
				t.Fatalf("budget leaked between calls: %+v %v", result, err)
			}
		})
	}
}

func TestCELConditionCostBoundary(t *testing.T) {
	env, err := newCELEnvironment(loadFixture[Schema](t, "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	checked, issues := env.Compile("amount_minor > 0")
	if issues.Err() != nil {
		t.Fatal(issues)
	}
	measured, err := env.Program(checked, cel.CostTracking(nil))
	if err != nil {
		t.Fatal(err)
	}
	_, details, err := measured.Eval(benchmarkEvaluation().Context)
	if err != nil {
		t.Fatal(err)
	}
	cost := *details.ActualCost()
	if cost == 0 {
		t.Fatal("cost was not tracked")
	}
	for _, budget := range []uint64{cost, cost - 1} {
		program, err := env.Program(checked, cel.CostLimit(budget))
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = program.Eval(benchmarkEvaluation().Context)
		if (err == nil) != (budget == cost) {
			t.Fatalf("budget %d, cost %d: %v", budget, cost, err)
		}
	}
}

func TestResourceLimitCannotBeMasked(t *testing.T) {
	for _, condition := range []string{
		"country.matches(supplier)", "matches(country,supplier)",
		"country.contains(supplier) || true",
		"dyn(country).matches(dyn(supplier)) || true",
		"country.matches(supplier + supplier) || true",
		"country.matches('" + strings.Repeat("a", maxRegexBytes+1) + "') || true",
	} {
		t.Run(condition, func(t *testing.T) {
			program, report := Compile(loadFixture[Schema](t, "schema.json"), minimalRuleSet(condition))
			if program == nil {
				t.Fatal(report)
			}
			input := benchmarkEvaluation()
			input.Context["country"] = strings.Repeat("a", maxStringBytes)
			input.Context["supplier"] = strings.Repeat("a", maxRegexBytes+1)
			result, err := program.Evaluate(input)
			if _, ok := err.(*EvaluationLimitError); !ok || result.Status != ResultLimitExceeded || result.Winner != nil {
				t.Fatalf("pattern limit masked: %+v %v", result, err)
			}
		})
	}
	program, report := Compile(loadFixture[Schema](t, "schema.json"), minimalRuleSet("country.matches(supplier)"))
	if program == nil {
		t.Fatal(report)
	}
	input := benchmarkEvaluation()
	input.Context["country"] = strings.Repeat("a", maxRegexBytes)
	input.Context["supplier"] = input.Context["country"]
	if result, err := program.Evaluate(input); err != nil || result.Status != ResultMatch {
		t.Fatalf("pattern boundary rejected: %+v %v", result, err)
	}
}
