package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestInvalidEvaluationCannotActivateFallback(t *testing.T) {
	program := rankingProgram(t, pctRule("FALLBACK", 0, nil))
	at := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	valid := Evaluation{Layer: "operation", EffectiveAt: at, Context: fixtureContext(nil)}
	if result, err := program.Evaluate(valid); err != nil || result.Winner == nil {
		t.Fatalf("valid input must activate fallback: %+v, %v", result, err)
	}
	cases := []struct {
		name string
		edit func(*Evaluation)
		path string
		code IssueCode
	}{
		{"empty layer", func(in *Evaluation) { in.Layer = "" }, "/layer", IssueRequired},
		{"blank layer", func(in *Evaluation) { in.Layer = " " }, "/layer", IssueRequired},
		{"unknown layer", func(in *Evaluation) { in.Layer = "missing" }, "/layer", IssueSchemaMismatch},
		{"layer is exact", func(in *Evaluation) { in.Layer = " operation " }, "/layer", IssueSchemaMismatch},
		{"missing date", func(in *Evaluation) { in.EffectiveAt = time.Time{} }, "/effective_at", IssueRequired},
		{"date out of range", func(in *Evaluation) { in.EffectiveAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }, "/effective_at", IssueInvalidRange},
		{"nil context", func(in *Evaluation) { in.Context = nil }, "/context", IssueRequired},
		{"missing unused variable", func(in *Evaluation) { delete(in.Context, "active") }, "/context/active", IssueRequired},
		{"null variable", func(in *Evaluation) { in.Context["active"] = nil }, "/context/active", IssueRequired},
		{"wrong bool", func(in *Evaluation) { in.Context["active"] = "SECRET" }, "/context/active", IssueSchemaMismatch},
		{"wrong string", func(in *Evaluation) { in.Context["country"] = true }, "/context/country", IssueSchemaMismatch},
		{"wrong int", func(in *Evaluation) { in.Context["amount_minor"] = "SECRET" }, "/context/amount_minor", IssueSchemaMismatch},
		{"wrong timestamp", func(in *Evaluation) { in.Context["travel_date"] = "SECRET" }, "/context/travel_date", IssueSchemaMismatch},
		{"zero timestamp", func(in *Evaluation) { in.Context["travel_date"] = time.Time{} }, "/context/travel_date", IssueSchemaMismatch},
		{"unknown variable", func(in *Evaluation) { in.Context["extra/~"] = "SECRET" }, "/context/extra~1~0", IssueSchemaMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := Evaluation{Layer: valid.Layer, EffectiveAt: at, Context: fixtureContext(nil)}
			tc.edit(&in)
			result, err := program.Evaluate(in)
			var invalid *InvalidEvaluationError
			if !errors.As(err, &invalid) {
				t.Fatalf("expected InvalidEvaluationError, got %v", err)
			}
			if result.Status != ResultInvalidInput || result.Winner != nil || result.Trace.Candidates == nil || len(result.Trace.Candidates) != 0 {
				t.Fatalf("invalid input must fail before any candidate: %+v", result)
			}
			found := false
			for _, issue := range invalid.Report.Issues {
				found = found || issue.Path == tc.path && issue.Code == tc.code
			}
			if !found {
				t.Fatalf("missing %s at %s: %+v", tc.code, tc.path, invalid.Report)
			}
			encoded, _ := json.Marshal(invalid.Report)
			if strings.Contains(string(encoded)+err.Error(), "SECRET") {
				t.Fatal("validation leaked a context value")
			}
		})
	}
}

func TestEvaluationNormalizesJSONWithoutMutatingInput(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	set := minimalRuleSet(`active && amount_minor == 42 && country == 'MX' && travel_date == timestamp('2026-07-01T00:00:00.123456789Z')`)
	set.Rules[0].Priority = 1
	set.Rules = append(set.Rules, pctRule("FALLBACK", 0, nil))
	program, report := Compile(schema, set)
	if program == nil {
		t.Fatal(report)
	}
	in := Evaluation{
		Layer: "operation", EffectiveAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Context: fixtureContext(map[string]any{"country": "MX", "amount_minor": int64(42), "active": true, "travel_date": "2026-06-30T18:00:00.123456789-06:00"}),
	}
	payload, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, useNumber := range []bool{false, true} {
		t.Run(fmt.Sprintf("UseNumber=%t", useNumber), func(t *testing.T) {
			decoder := json.NewDecoder(strings.NewReader(string(payload)))
			if useNumber {
				decoder.UseNumber()
			}
			var decoded Evaluation
			if err := decoder.Decode(&decoded); err != nil {
				t.Fatal(err)
			}
			before := fixtureContext(decoded.Context)
			result, err := program.Evaluate(decoded)
			if err != nil || result.Winner == nil || result.Winner.RuleID != "BASE" {
				t.Fatalf("normalized values must match CEL, not fallback: %+v, %v", result, err)
			}
			if !reflect.DeepEqual(decoded.Context, before) {
				t.Fatal("Evaluate mutated the input")
			}
			decoded.Context["active"] = "invalid"
			before = fixtureContext(decoded.Context)
			if _, err := program.Evaluate(decoded); err == nil {
				t.Fatal("invalid input was accepted")
			}
			if !reflect.DeepEqual(decoded.Context, before) {
				t.Fatal("invalid input was mutated")
			}
		})
	}
}

func TestEvaluationIntegerBoundaries(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  int64
		valid bool
	}{
		{"int", int(42), 42, true}, {"int8", int8(-42), -42, true},
		{"int16", int16(42), 42, true}, {"int32", int32(42), 42, true},
		{"min int64", int64(math.MinInt64), math.MinInt64, true},
		{"max int64", int64(math.MaxInt64), math.MaxInt64, true},
		{"uint", uint(42), 42, true}, {"uint8", uint8(42), 42, true},
		{"uint16", uint16(42), 42, true}, {"uint32", uint32(42), 42, true},
		{"max uint64 in range", uint64(math.MaxInt64), math.MaxInt64, true},
		{"overflow uint64", uint64(math.MaxInt64) + 1, 0, false},
		{"exact JSON max", json.Number("9223372036854775807"), math.MaxInt64, true},
		{"exact JSON min", json.Number("-9223372036854775808"), math.MinInt64, true},
		{"JSON overflow", json.Number("9223372036854775808"), 0, false},
		{"JSON fractional", json.Number("1.5"), 0, false},
		{"JSON exponent", json.Number("1e3"), 0, false},
		{"JSON leading zero", json.Number("042"), 0, false},
		{"JSON plus sign", json.Number("+42"), 0, false},
		{"JSON malformed", json.Number("invalid"), 0, false},
		{"JSON negative zero", json.Number("-0"), 0, true},
		{"safe float", float64(1<<53 - 1), 1<<53 - 1, true},
		{"safe negative float", -float64(1<<53 - 1), -(1<<53 - 1), true},
		{"unsafe float", float64(1 << 53), 0, false},
		{"unsafe negative float", -float64(1 << 53), 0, false},
		{"fraction", 42.5, 0, false}, {"NaN", math.NaN(), 0, false},
		{"infinity", math.Inf(1), 0, false}, {"negative infinity", math.Inf(-1), 0, false},
		{"float32", float32(42), 0, false}, {"numeric string", "42", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schema := loadFixture[Schema](t, "schema.json")
			program, report := Compile(schema, minimalRuleSet(fmt.Sprintf("amount_minor == %d", tc.want)))
			if program == nil {
				t.Fatal(report)
			}
			result, err := program.Evaluate(Evaluation{Layer: "operation", EffectiveAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Context: fixtureContext(map[string]any{"amount_minor": tc.value})})
			if tc.valid {
				if err != nil || result.Status != ResultMatch {
					t.Fatalf("integer changed or failed CEL: %+v, %v", result, err)
				}
			} else {
				var invalid *InvalidEvaluationError
				if !errors.As(err, &invalid) || result.Status != ResultInvalidInput {
					t.Fatalf("unsafe integer accepted: %+v, %v", result, err)
				}
			}
		})
	}
}

func TestEvaluationValidationReportIsDeterministic(t *testing.T) {
	program := rankingProgram(t, pctRule("FALLBACK", 0, nil))
	input := Evaluation{Context: map[string]any{"z": "secret", "a": nil}}
	_, first := program.Evaluate(input)
	for i := 0; i < 20; i++ {
		input.Context = reverseMap(input.Context)
		_, next := program.Evaluate(input)
		if !reflect.DeepEqual(first, next) {
			t.Fatalf("unstable errors: %v, %v", first, next)
		}
	}
}

func TestEvaluationTimestampBoundaries(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	program, report := Compile(schema, minimalRuleSet(`travel_date >= timestamp('0001-01-01T00:00:00Z')`))
	if program == nil {
		t.Fatal(report)
	}
	cases := []struct {
		name  string
		value any
		valid bool
	}{
		{"native timestamp", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), true},
		{"minimum nonzero", "0001-01-01T00:00:00.000000001Z", true},
		{"maximum", "9999-12-31T23:59:59.999999999Z", true},
		{"zero string", "0001-01-01T00:00:00Z", false},
		{"year zero", time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), false},
		{"year too large", time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), false},
		{"UTC overflow", "9999-12-31T23:59:59-01:00", false},
		{"UTC underflow", "0001-01-01T00:00:00+01:00", false},
		{"date only", "2026-07-01", false},
		{"no timezone", "2026-07-01T00:00:00", false},
		{"unix seconds", int64(1782864000), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := Evaluation{Layer: "operation", EffectiveAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Context: fixtureContext(map[string]any{"travel_date": tc.value})}
			result, err := program.Evaluate(input)
			if tc.valid {
				if err != nil || result.Status != ResultMatch {
					t.Fatalf("timestamp failed CEL: %+v, %v", result, err)
				}
			} else if err == nil || result.Status != ResultInvalidInput {
				t.Fatalf("invalid timestamp accepted: %+v, %v", result, err)
			}
		})
	}
}
