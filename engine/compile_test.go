package engine

import (
	"fmt"
	"github.com/google/cel-go/cel"
	"reflect"
	"strings"
	"testing"
)

func TestCompileSharedConditions(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	rules := benchmarkRules(3)
	rules.Rules[2].Layer = "other"
	program, report := Compile(schema, rules)
	if program == nil {
		t.Fatal(report)
	}
	shared := program.layers["operation"][0].program
	if shared != program.layers["operation"][1].program || shared != program.layers["other"][0].program {
		t.Fatal("identical conditions should share one CEL program across layers")
	}
	for _, active := range []bool{true, false, true} {
		input := benchmarkEvaluation()
		input.Context["active"] = active
		result, err := program.Evaluate(input)
		if err != nil || (result.Winner != nil) != active {
			t.Fatalf("active=%v: %+v, %v", active, result, err)
		}
		if active && result.Winner.RuleID != "RULE_00001" {
			t.Fatal(result)
		}
		if len(result.Trace.Candidates) != 2 {
			t.Fatal("sharing must preserve per-rule traces")
		}
	}
	// Reuse must never cross schema boundaries between Compile calls.
	schema.Variables["active"] = ValueString
	if other, report := Compile(schema, rules); other != nil || report.Valid() {
		t.Fatal("condition reused across incompatible schemas")
	}

	schema.Variables["active"] = ValueBool
	for _, condition := range []string{"active &&", "42"} {
		for i := range rules.Rules {
			rules.Rules[i].Condition = condition
		}
		other, report := Compile(schema, rules)
		if other != nil || report.Valid() {
			t.Fatal("invalid condition accepted")
		}
		for i := range rules.Rules {
			found := false
			for _, issue := range report.Issues {
				if issue.Path == fmt.Sprintf("/rules/%d/condition", i) {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing issue for rule %d: %+v", i, report)
			}
		}
	}
}

// Compare the reduced planning environment with CEL's full environment,
// including dynamic overloads, nested calls, type values and runtime errors.
func TestConditionEnvironmentEquivalent(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	full, err := newCELEnvironment(schema)
	if err != nil {
		t.Fatal(err)
	}
	cache := make(map[string]*cel.Env)
	conditions := []string{
		"true", "active", "type(amount_minor) == int",
		"active ? amount_minor > 1 : country == 'MX'",
		"dyn(amount_minor) >= 100000 && dyn(country) == 'MX'",
		"dyn(country).contains('M') || !active",
		"country in ['MX', 'US'] && {'MX': 1}[country] == 1",
		"size(country) > 0 && country.startsWith('M') && country.endsWith('X')",
		"country.matches('^[A-Z]+$')",
		"int(country) > 0", "amount_minor / 0 == 1",
		"(amount_minor + 2) * 3 % 7 != -1",
		"string(amount_minor) == '200000' && double(amount_minor) > 0.0",
		"travel_date >= timestamp('2026-01-01T00:00:00Z') && travel_date.getFullYear() == 2026",
		"travel_date + duration('1h') > travel_date",
		"bytes(country) == b'MX'",
	}
	for _, expression := range conditions {
		checked, issues := full.Compile(expression)
		if issues.Err() != nil {
			t.Fatalf("%s: %v", expression, issues.Err())
		}
		reduced, err := conditionEnvironment(full, checked, cache)
		if err != nil {
			t.Fatal(err)
		}
		want, err := full.Program(checked)
		if err != nil {
			t.Fatal(err)
		}
		got, err := reduced.Program(checked)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(expression, func(t *testing.T) {
			t.Parallel()
			for i := 0; i < 32; i++ {
				input := benchmarkEvaluation().Context
				input["active"] = i%2 == 0
				if i%3 == 0 {
					input["country"] = "123"
					input["amount_minor"] = int64(0)
				}
				expected, _, expectedErr := want.Eval(input)
				actual, _, actualErr := got.Eval(input)
				if fmt.Sprint(actualErr) != fmt.Sprint(expectedErr) || !reflect.DeepEqual(actual, expected) {
					t.Fatalf("reduced environment changed result: %v / %v; expected %v / %v", actual, actualErr, expected, expectedErr)
				}
			}
		})
	}
}

func TestConditionReuseMatchesIndependentEvaluation(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	for _, expression := range []string{"true", "active", "country.contains(supplier)", "int(country)>0", "country.matches(supplier)"} {
		t.Run(expression, func(t *testing.T) {
			rules := benchmarkRules(3000)
			for i := range rules.Rules {
				rules.Rules[i].Scope = nil
				rules.Rules[i].Condition = expression
			}
			program, report := Compile(schema, rules)
			if program == nil {
				t.Fatal(report)
			}
			independent := *program
			independent.layers = map[string][]compiledRule{}
			for layer, candidates := range program.layers {
				independent.layers[layer] = append([]compiledRule(nil), candidates...)
				for i := range independent.layers[layer] {
					independent.layers[layer][i].reuseCondition = false
				}
			}
			for _, values := range []map[string]any{
				{"active": true, "country": "123", "supplier": "1"},
				{"active": false, "country": "MX", "supplier": "hotelbeds"},
				{"country": strings.Repeat("a", maxStringBytes), "supplier": "a"},
				{"country": strings.Repeat("a", maxStringBytes), "supplier": strings.Repeat("a", maxStringBytes)},
			} {
				input := benchmarkEvaluation()
				input.Context = fixtureContext(values)
				got, gotErr := program.Evaluate(input)
				want, wantErr := independent.Evaluate(input)
				if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotErr, wantErr) {
					t.Fatalf("reuse changed resolution or budget: %+v / %v; want %+v / %v", got, gotErr, want, wantErr)
				}
			}
		})
	}
}
