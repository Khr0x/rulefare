package engine

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// Bound fuzz-generated documents so short campaigns explore behavior rather
// than allocation stress. This is a harness bound, not a production limit.
const maxFuzzBytes = 64 << 10

func FuzzCompileRuleset(f *testing.F) {
	schema := loadFixture[Schema](f, "schema.json")
	for _, name := range []string{
		"ruleset.json", "invalid_dates.json", "invalid_stack.json",
		"invalid_non_bool.json", "invalid_cel.json", "invalid_outcome.json",
		"invalid_unknown_variable.json", "invalid_tiers.json", "invalid_unknown_dimension.json",
	} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	for _, seed := range []string{"", "{", "null", "{}", "[]", `{"rules":[null]}`, `{} {}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var rules RuleSet
		if !decodeFuzzJSON(data, &rules) {
			return
		}
		checkFuzzCompile(t, schema, rules)
		var original RuleSet
		if !decodeFuzzJSON(data, &original) || !reflect.DeepEqual(rules, original) {
			t.Fatal("Compile mutated the ruleset")
		}
	})
}

// Mutate CEL and scope independently of JSON syntax so the compiler is
// exercised even when raw document mutations mostly fail JSON decoding.
func FuzzRuleCondition(f *testing.F) {
	schema := loadFixture[Schema](f, "schema.json")
	for _, seed := range [][3]string{
		{"amount_minor > 100000", "country", "MX"},
		{"true", "country", "US"}, {"false", "", ""},
		{"active", "unknown", "MX"}, {"int(country) > 0", "country", "MX"},
		{"amount_minor / 0 > 0", "", ""}, {"timestamp(country) > travel_date", "", ""},
		{"", "country", "MX"}, {"1", "country", "MX"}, {"(", "country", "MX"},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}
	f.Fuzz(func(t *testing.T, condition, dimension, value string) {
		if len(condition)+len(dimension)+len(value) > maxFuzzBytes {
			return
		}
		rules := minimalRuleSet(condition)
		if dimension != "" {
			rules.Rules[0].Scope = map[string]string{dimension: value}
		}
		before := fuzzJSON(t, rules)
		checkFuzzCompile(t, schema, rules)
		if !bytes.Equal(before, fuzzJSON(t, rules)) {
			t.Fatal("Compile mutated the ruleset")
		}
	})
}

func FuzzEvaluateContext(f *testing.F) {
	rules := loadFixture[RuleSet](f, "ruleset.json")
	for _, id := range []string{"TIE_A", "TIE_B"} {
		rule := pctRule(id, 0, nil)
		rule.Layer = "ambiguous"
		rules.Rules = append(rules.Rules, rule)
	}
	broken := pctRule("CONVERSION", 0, nil)
	broken.Layer, broken.Condition = "error", "int(country) > 0"
	rules.Rules = append(rules.Rules, broken)
	program, report := Compile(loadFixture[Schema](f, "schema.json"), rules)
	if program == nil {
		f.Fatal(report)
	}
	for _, seed := range []string{
		"", "{", "null", "{}", "[]", `{} {}`,
		`{"layer":"operation","effective_at":"invalid","context":{}}`,
	} {
		f.Add([]byte(seed))
	}
	// Seeds deliberately reach every result status and all three outcome types.
	for _, layer := range []string{"operation", "split", "missing", "ambiguous", "error"} {
		for _, country := range []string{"MX", "US"} {
			input := fuzzEvaluation(layer)
			input.Context["country"] = country
			data, err := json.Marshal(input)
			if err != nil {
				f.Fatal(err)
			}
			f.Add(data)
		}
	}
	for _, value := range []any{
		json.Number("9223372036854775807"), json.Number("9223372036854775808"),
		json.Number("-9223372036854775808"), json.Number("1.5"),
		nil, "not-an-integer", []any{}, map[string]any{}, json.Number("123456789012345678901"),
	} {
		input := fuzzEvaluation("operation")
		input.Context["amount_minor"] = value
		data, err := json.Marshal(input)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	oversized := fuzzEvaluation("operation")
	oversized.Context["country"] = strings.Repeat("x", maxStringBytes+1)
	data, err := json.Marshal(oversized)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Fuzz(func(t *testing.T, data []byte) {
		var input Evaluation
		if !decodeFuzzJSON(data, &input) {
			return
		}
		checkFuzzEvaluation(t, program, input)
	})
}

func decodeFuzzJSON(data []byte, target any) bool {
	if len(data) > maxFuzzBytes {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	return decoder.Decode(target) == nil && decoder.Decode(new(any)) == io.EOF
}

func fuzzEvaluation(layer string) Evaluation {
	return Evaluation{
		Layer: layer, EffectiveAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Context: fixtureContext(map[string]any{
			"country": "MX", "supplier": "hotelbeds", "contract": "ctr_hb_2026",
			"amount_minor": int64(200000), "active": true,
		}),
	}
}

func checkFuzzCompile(t *testing.T, schema Schema, rules RuleSet) {
	t.Helper()
	program, report := Compile(schema, rules)
	if (program != nil) != report.Valid() {
		t.Fatalf("program/report disagree: %+v", report)
	}
	reordered := rules
	if report.Valid() {
		reordered.Rules = append([]Rule(nil), rules.Rules...)
		slices.Reverse(reordered.Rules)
	}
	other, otherReport := Compile(schema, reordered)
	if !reflect.DeepEqual(report, otherReport) || (program == nil) != (other == nil) {
		t.Fatal("compilation is not deterministic")
	}
	if program == nil {
		return
	}
	for _, layer := range []string{"operation", "split"} {
		input := fuzzEvaluation(layer)
		result, err := program.Evaluate(input)
		otherResult, otherErr := other.Evaluate(input)
		if !bytes.Equal(fuzzJSON(t, result), fuzzJSON(t, otherResult)) || !reflect.DeepEqual(err, otherErr) {
			t.Fatal("rule order changed resolution or trace")
		}
		checkFuzzEvaluation(t, program, input)
	}
}

func checkFuzzEvaluation(t *testing.T, program *Program, input Evaluation) {
	t.Helper()
	// Context is the only reference-valued part of Evaluation. Check it
	// directly; an invalid EffectiveAt need not support a JSON round-trip.
	before := fuzzJSON(t, input.Context)
	result, err := program.Evaluate(input)
	switch result.Status {
	case ResultMatch:
		if err != nil || result.Winner == nil {
			t.Fatal("MATCH without a winner or with an error")
		}
	case ResultNoMatch:
		if err != nil || result.Winner != nil {
			t.Fatal("NO_MATCH with a winner or error")
		}
	case ResultInvalidInput:
		if _, ok := err.(*InvalidEvaluationError); !ok || result.Winner != nil || len(result.Trace.Candidates) != 0 {
			t.Fatal("invalid input reached rule resolution")
		}
	case ResultLimitExceeded:
		if _, ok := err.(*EvaluationLimitError); !ok || result.Winner != nil || len(result.Trace.Candidates) != 0 {
			t.Fatal("cost limit did not fail closed")
		}
	case ResultAmbiguous:
		if _, ok := err.(*AmbiguousMatchError); !ok || result.Winner != nil {
			t.Fatal("ambiguous input did not fail closed")
		}
	default:
		t.Fatalf("unknown status: %s", result.Status)
	}
	for _, entry := range result.Trace.Candidates {
		if entry.Status == TraceConditionError && entry.Reason != "" {
			t.Fatal("CEL error text leaked into trace")
		}
	}
	wantResult, wantError := fuzzJSON(t, result), fuzzJSON(t, err)
	mutateEvaluationOutput(&result, err)
	next, nextErr := program.Evaluate(input)
	if !bytes.Equal(wantResult, fuzzJSON(t, next)) || !bytes.Equal(wantError, fuzzJSON(t, nextErr)) {
		t.Fatal("mutating outputs changed the program or evaluation is not deterministic")
	}
	if !bytes.Equal(before, fuzzJSON(t, input.Context)) {
		t.Fatal("Evaluate mutated input")
	}
}

func fuzzJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
