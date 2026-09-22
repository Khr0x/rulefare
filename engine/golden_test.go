package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"slices"
	"testing"
)

// Expectations are reviewed data, never regenerated from Evaluate. Business
// approval is tracked separately in testdata/README.md; passing is technical evidence.
func TestGoldenEvaluations(t *testing.T) {
	data, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		ReviewStatus string `json:"review_status"`
		Schema       string `json:"schema"`
		Cases        []struct {
			Name            string            `json:"name"`
			Description     string            `json:"description"`
			Source          string            `json:"source"`
			RuleSet         string            `json:"ruleset"`
			Input           Evaluation        `json:"input"`
			Expected        Result            `json:"expected"`
			ExpectedError   string            `json:"expected_error"`
			ExpectedIssues  []ValidationIssue `json:"expected_issues"`
			ExpectedRuleIDs []string          `json:"expected_rule_ids"`
		} `json:"cases"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("unexpected trailing JSON: %v", err)
	}
	if len(corpus.Cases) == 0 {
		t.Fatal("empty golden corpus")
	}
	schema := loadFixture[Schema](t, corpus.Schema)
	seen := make(map[string]bool)
	for _, tc := range corpus.Cases {
		if tc.Name == "" || seen[tc.Name] || tc.Source == "" || tc.Description == "" {
			t.Fatalf("invalid case metadata: %q", tc.Name)
		}
		seen[tc.Name] = true
		t.Run(tc.Name, func(t *testing.T) {
			rules := loadFixture[RuleSet](t, tc.RuleSet)
			want, err := json.Marshal(tc.Expected)
			if err != nil {
				t.Fatal(err)
			}
			for _, order := range []string{"original", "reversed"} {
				t.Run(order, func(t *testing.T) {
					if order == "reversed" {
						slices.Reverse(rules.Rules)
					}
					program, report := Compile(schema, rules)
					if !report.Valid() || program == nil {
						t.Fatalf("compile: %+v", report)
					}
					result, err := program.Evaluate(tc.Input)
					switch tc.ExpectedError {
					case "":
						if err != nil || len(tc.ExpectedIssues) != 0 {
							t.Fatalf("unexpected error or expected issues: %v", err)
						}
					case "AmbiguousMatchError":
						var ambiguous *AmbiguousMatchError
						if !errors.As(err, &ambiguous) || ambiguous.Layer != tc.Input.Layer || !reflect.DeepEqual(ambiguous.RuleIDs, tc.ExpectedRuleIDs) {
							t.Fatalf("unexpected ambiguity: %+v, want IDs %v", err, tc.ExpectedRuleIDs)
						}
					case "EvaluationLimitError":
						var limit *EvaluationLimitError
						if !errors.As(err, &limit) || !reflect.DeepEqual([]string{limit.RuleID}, tc.ExpectedRuleIDs) {
							t.Fatalf("unexpected resource error: %+v, want IDs %v", err, tc.ExpectedRuleIDs)
						}
					case "InvalidEvaluationError":
						var invalid *InvalidEvaluationError
						if !errors.As(err, &invalid) {
							t.Fatalf("want InvalidEvaluationError, got %v", err)
						}
						if !reflect.DeepEqual(invalid.Report.Issues, tc.ExpectedIssues) {
							t.Fatalf("issues: got %+v, want %+v", invalid.Report.Issues, tc.ExpectedIssues)
						}
					default:
						t.Fatalf("unsupported expected error: %q", tc.ExpectedError)
					}
					got, err := json.Marshal(result)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, want) {
						t.Fatalf("result snapshot differs\nwant %s\n got %s", want, got)
					}
				})
			}
		})
	}
}
