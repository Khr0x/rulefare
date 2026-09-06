package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
)

func TestContractJSONRoundTrip(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	rules := loadFixture[RuleSet](t, "ruleset.json")

	encoded, err := json.Marshal(rules)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"rate":"0.17"`)) {
		t.Fatalf("percentage rate must remain a JSON string: %s", encoded)
	}
	if !bytes.Contains(encoded, []byte(`"valid_from":"2026-06-01T00:00:00Z"`)) {
		t.Fatalf("time must use RFC3339: %s", encoded)
	}

	var decoded RuleSet
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rules, decoded) {
		t.Fatalf("ruleset changed after JSON round trip\nwant: %#v\n got: %#v", rules, decoded)
	}
	if program, report := Compile(schema, decoded); program == nil || !report.Valid() {
		t.Fatalf("round-tripped contract did not compile: %+v", report.Issues)
	}
}

func TestCompileCorpus(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	valid := loadFixture[RuleSet](t, "ruleset.json")
	if program, report := Compile(schema, valid); program == nil || !report.Valid() {
		t.Fatalf("valid corpus did not compile: %+v", report.Issues)
	}

	tests := []struct {
		file string
		code IssueCode
	}{
		{"invalid_unknown_dimension.json", IssueUnknownDimension},
		{"invalid_dates.json", IssueInvalidRange},
		{"invalid_cel.json", IssueInvalidCEL},
		{"invalid_non_bool.json", IssueCELNotBool},
		{"invalid_unknown_variable.json", IssueInvalidCEL},
		{"invalid_outcome.json", IssueInvalidOutcome},
		{"invalid_tiers.json", IssueInvalidRange},
		{"invalid_stack.json", IssueUnsupportedComposition},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			set := loadFixture[RuleSet](t, test.file)
			program, report := Compile(schema, set)
			if program != nil || report.Valid() {
				t.Fatalf("invalid fixture compiled: %+v", report.Issues)
			}
			if len(report.Issues) != 1 || report.Issues[0].Code != test.code {
				t.Fatalf("expected exactly %s, got %+v", test.code, report.Issues)
			}
		})
	}
}

func TestValidationCodes(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Schema, *RuleSet)
		code   IssueCode
	}{
		{
			name: "required",
			change: func(_ *Schema, set *RuleSet) {
				set.ID = ""
			},
			code: IssueRequired,
		},
		{
			name: "duplicate",
			change: func(_ *Schema, set *RuleSet) {
				set.Dimensions = append(set.Dimensions, "country")
			},
			code: IssueDuplicate,
		},
		{
			name: "unknown type",
			change: func(schema *Schema, _ *RuleSet) {
				schema.Variables["amount_minor"] = "double"
			},
			code: IssueUnknownType,
		},
		{
			name: "schema mismatch",
			change: func(_ *Schema, set *RuleSet) {
				set.SchemaVersion = "v2"
			},
			code: IssueSchemaMismatch,
		},
		{
			name: "condition limit",
			change: func(_ *Schema, set *RuleSet) {
				set.Rules[0].Condition = strings.Repeat("x", maxConditionBytes+1)
			},
			code: IssueLimitExceeded,
		},
		{
			name: "non canonical percentage",
			change: func(_ *Schema, set *RuleSet) {
				set.Rules[0].Outcome.Percentage.Rate = "0.10"
			},
			code: IssueInvalidOutcome,
		},
		{
			name: "percentage over one",
			change: func(_ *Schema, set *RuleSet) {
				set.Rules[0].Outcome.Percentage.Rate = "1.1"
			},
			code: IssueInvalidOutcome,
		},
		{
			name: "fixed currency",
			change: func(_ *Schema, set *RuleSet) {
				set.Rules[2].Outcome.Fixed.Currency = "mxn"
			},
			code: IssueInvalidOutcome,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schema := loadFixture[Schema](t, "schema.json")
			set := loadFixture[RuleSet](t, "ruleset.json")
			test.change(&schema, &set)
			program, report := Compile(schema, set)
			if program != nil || !reportHasCode(report, test.code) {
				t.Fatalf("expected %s, got program=%v report=%+v", test.code, program, report.Issues)
			}
		})
	}
}

func TestCELContract(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	tests := []struct {
		name      string
		condition string
		valid     bool
		code      IssueCode
	}{
		{
			name:      "all supported types",
			condition: `active && amount_minor > 0 && country == "MX" && travel_date < timestamp("2027-01-01T00:00:00Z")`,
			valid:     true,
		},
		{name: "empty means true", condition: "", valid: true},
		{name: "macros disabled", condition: `[1, 2].exists(x, x > 0)`, code: IssueInvalidCEL},
		{name: "node limit", condition: strings.Repeat("true && ", 300) + "true", code: IssueInvalidCEL},
		{name: "nesting limit", condition: strings.Repeat("(", 40) + "true" + strings.Repeat(")", 40), code: IssueInvalidCEL},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			set := minimalRuleSet(test.condition)
			program, report := Compile(schema, set)
			if test.valid {
				if program == nil || !report.Valid() {
					t.Fatalf("valid CEL failed: %+v", report.Issues)
				}
				return
			}
			if program != nil || !reportHasCode(report, test.code) {
				t.Fatalf("expected %s, got program=%v report=%+v", test.code, program, report.Issues)
			}
		})
	}
}

func TestCompileStopsBeforeCELOnStructuralErrors(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	set := minimalRuleSet("not valid CEL !!!")
	set.ID = ""

	program, report := Compile(schema, set)
	if program != nil || !reportHasCode(report, IssueRequired) {
		t.Fatalf("expected structural failure: %+v", report.Issues)
	}
	if reportHasCode(report, IssueInvalidCEL) {
		t.Fatalf("CEL must not compile after structural failure: %+v", report.Issues)
	}
}

func TestProgramIsDeeplyImmutable(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	set := loadFixture[RuleSet](t, "ruleset.json")
	program, report := Compile(schema, set)
	if program == nil || !report.Valid() {
		t.Fatalf("compile failed: %+v", report.Issues)
	}

	schema.Variables["country"] = ValueInt
	set.Dimensions[0] = "changed"
	set.Rules[0].Outcome.Percentage.Rate = "0.99"
	set.Rules[1].Scope["country"] = "US"
	*set.Rules[4].Outcome.Tiered.Tiers[0].UpToExclusive = 1

	if program.schema.Variables["country"] != ValueString {
		t.Fatal("compiled schema shares caller memory")
	}
	if program.dimensions[0] != "market" {
		t.Fatal("compiled dimensions share caller memory")
	}
	base := compiledByID(t, program, "operation", "BASE_GLOBAL")
	if base.rule.Outcome.Percentage.Rate != "0.12" {
		t.Fatal("compiled percentage shares caller memory")
	}
	if base.rule.Composition != CompositionFirstMatch {
		t.Fatalf("default composition was not normalized: %q", base.rule.Composition)
	}
	mexico := compiledByID(t, program, "operation", "MEXICO")
	if mexico.rule.Scope["country"] != "MX" {
		t.Fatal("compiled scope shares caller memory")
	}
	tiered := compiledByID(t, program, "split", "CONTRACT_TIERS")
	if got := *tiered.rule.Outcome.Tiered.Tiers[0].UpToExclusive; got != 100000 {
		t.Fatalf("compiled tier shares caller memory: %d", got)
	}

	operationIDs := make([]string, len(program.layers["operation"]))
	for i, rule := range program.layers["operation"] {
		operationIDs[i] = rule.rule.ID
	}
	want := []string{"BASE_GLOBAL", "HOTELBEDS_FIXED", "HOTELBEDS_MX", "MEXICO"}
	if !reflect.DeepEqual(operationIDs, want) {
		t.Fatalf("compiled layer is not sorted\nwant: %v\n got: %v", want, operationIDs)
	}
}

func TestValidationReportIsDeterministic(t *testing.T) {
	set := RuleSet{Rules: []Rule{{}}}
	schemaA := Schema{Variables: map[string]ValueType{"Bad/B": "double", "Bad~A": "double"}}
	schemaB := Schema{Variables: map[string]ValueType{}}
	schemaB.Variables["Bad~A"] = "double"
	schemaB.Variables["Bad/B"] = "double"

	_, reportA := Compile(schemaA, set)
	_, reportB := Compile(schemaB, set)
	if !reflect.DeepEqual(reportA, reportB) {
		t.Fatalf("reports depend on map insertion order\nA: %+v\nB: %+v", reportA.Issues, reportB.Issues)
	}
	for i := 1; i < len(reportA.Issues); i++ {
		previous, current := reportA.Issues[i-1], reportA.Issues[i]
		if previous.Path > current.Path || previous.Path == current.Path && previous.Code > current.Code {
			t.Fatalf("report is not sorted at %d: %+v then %+v", i, previous, current)
		}
	}
}

func TestProgramMetadataIsDeterministic(t *testing.T) {
	schemaA := loadFixture[Schema](t, "schema.json")
	schemaB := schemaA
	schemaB.Variables = reverseMap(schemaA.Variables)
	setA := loadFixture[RuleSet](t, "ruleset.json")
	setB := loadFixture[RuleSet](t, "ruleset.json")
	for i := range setB.Rules {
		setB.Rules[i].Scope = reverseMap(setB.Rules[i].Scope)
	}

	programA, reportA := Compile(schemaA, setA)
	programB, reportB := Compile(schemaB, setB)
	if programA == nil || programB == nil || !reportA.Valid() || !reportB.Valid() {
		t.Fatalf("valid inputs failed: A=%+v B=%+v", reportA.Issues, reportB.Issues)
	}
	if !reflect.DeepEqual(snapshotProgram(programA), snapshotProgram(programB)) {
		t.Fatal("compiled metadata depends on map insertion order")
	}
}

func TestValidationIssueLimit(t *testing.T) {
	schema := Schema{Version: "v1", Variables: map[string]ValueType{}}
	set := RuleSet{ID: "limit", Version: "v1", SchemaVersion: "v1", Rules: make([]Rule, 1_100)}
	_, report := Compile(schema, set)
	if len(report.Issues) != maxValidationIssues {
		t.Fatalf("expected %d issues, got %d", maxValidationIssues, len(report.Issues))
	}
	count := 0
	for _, issue := range report.Issues {
		if issue.Code == IssueLimitExceeded && issue.Path == "" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one truncation issue, got %d", count)
	}
}

func minimalRuleSet(condition string) RuleSet {
	return RuleSet{
		ID:            "minimal",
		Version:       "v1",
		SchemaVersion: "v1",
		Dimensions:    []string{"country"},
		Rules: []Rule{{
			ID:        "BASE",
			Layer:     "operation",
			Condition: condition,
			Outcome: Outcome{
				Kind:       OutcomePercentage,
				Percentage: &PercentageOutcome{Rate: "0.1"},
			},
		}},
	}
}

func loadFixture[T any](t *testing.T, name string) T {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var value T
	if err = json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return value
}

func reportHasCode(report ValidationReport, code IssueCode) bool {
	for _, issue := range report.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func compiledByID(t *testing.T, program *Program, layer, id string) compiledRule {
	t.Helper()
	for _, rule := range program.layers[layer] {
		if rule.rule.ID == id {
			return rule
		}
	}
	t.Fatalf("rule %s not found in layer %s", id, layer)
	return compiledRule{}
}

type programSnapshot struct {
	Schema        Schema
	RuleSetID     string
	Version       string
	SchemaVersion string
	Dimensions    []string
	Layers        map[string][]Rule
}

func snapshotProgram(program *Program) programSnapshot {
	layers := make(map[string][]Rule, len(program.layers))
	for layer, compiled := range program.layers {
		rules := make([]Rule, len(compiled))
		for i := range compiled {
			rules[i] = compiled[i].rule
		}
		layers[layer] = rules
	}
	return programSnapshot{
		Schema:        program.schema,
		RuleSetID:     program.ruleSetID,
		Version:       program.version,
		SchemaVersion: program.schemaVersion,
		Dimensions:    program.dimensions,
		Layers:        layers,
	}
}

func reverseMap[V any](source map[string]V) map[string]V {
	keys := sortedKeys(source)
	reversed := make(map[string]V, len(source))
	for i := len(keys) - 1; i >= 0; i-- {
		reversed[keys[i]] = source[keys[i]]
	}
	return reversed
}

func TestValidToMustBeExclusiveBoundary(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	set := minimalRuleSet("")
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	set.Rules[0].ValidFrom = &start
	set.Rules[0].ValidTo = &start
	program, report := Compile(schema, set)
	if program != nil || !reportHasCode(report, IssueInvalidRange) {
		t.Fatalf("equal validity bounds must fail: %+v", report.Issues)
	}
}

func ExampleCompile() {
	schema := Schema{
		Version:   "v1",
		Variables: map[string]ValueType{"country": ValueString},
	}
	set := RuleSet{
		ID:            "example",
		Version:       "v1",
		SchemaVersion: "v1",
		Dimensions:    []string{"country"},
		Rules: []Rule{{
			ID:      "MEXICO",
			Layer:   "operation",
			Scope:   map[string]string{"country": "MX"},
			Outcome: Outcome{Kind: OutcomePercentage, Percentage: &PercentageOutcome{Rate: "0.12"}},
		}},
	}
	program, report := Compile(schema, set)
	fmt.Println(program != nil, report.Valid())
	// Output: true true
}

func timePtr(t time.Time) *time.Time { return &t }

func TestEffectiveBoundaries(t *testing.T) {
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		rule Rule
		at   time.Time
		want bool
	}{
		{"unbounded", Rule{}, from, true},
		{"before from", Rule{ValidFrom: timePtr(from)}, from.Add(-time.Second), false},
		{"exactly at from is inclusive", Rule{ValidFrom: timePtr(from)}, from, true},
		{"after from", Rule{ValidFrom: timePtr(from)}, from.Add(time.Second), true},
		{"before to", Rule{ValidTo: timePtr(to)}, to.Add(-time.Second), true},
		{"exactly at to is exclusive", Rule{ValidTo: timePtr(to)}, to, false},
		{"after to", Rule{ValidTo: timePtr(to)}, to.Add(time.Second), false},
		{"within both", Rule{ValidFrom: timePtr(from), ValidTo: timePtr(to)}, from, true},
		{"at to within both is excluded", Rule{ValidFrom: timePtr(from), ValidTo: timePtr(to)}, to, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effective(tc.rule, tc.at); got != tc.want {
				t.Fatalf("effective(%v) = %v, want %v", tc.at, got, tc.want)
			}
		})
	}
}

func TestScopeMatch(t *testing.T) {
	cases := []struct {
		name         string
		scope        map[string]string
		ctx          map[string]any
		wantOK       bool
		wantMismatch string
	}{
		{"empty scope is global", nil, map[string]any{"country": "MX"}, true, ""},
		{"single dimension match", map[string]string{"country": "MX"}, map[string]any{"country": "MX"}, true, ""},
		{"single dimension mismatch", map[string]string{"country": "MX"}, map[string]any{"country": "US"}, false, "country"},
		{"multiple all match", map[string]string{"country": "MX", "supplier": "hb"}, map[string]any{"country": "MX", "supplier": "hb"}, true, ""},
		{"multiple one mismatch", map[string]string{"country": "MX", "supplier": "hb"}, map[string]any{"country": "MX", "supplier": "x"}, false, "supplier"},
		{"dimension absent", map[string]string{"country": "MX"}, map[string]any{}, false, "country"},
		{"wrong type", map[string]string{"country": "MX"}, map[string]any{"country": 42}, false, "country"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, mismatch := scopeMatch(tc.scope, tc.ctx)
			if ok != tc.wantOK || mismatch != tc.wantMismatch {
				t.Fatalf("scopeMatch = (%v, %q), want (%v, %q)", ok, mismatch, tc.wantOK, tc.wantMismatch)
			}
		})
	}
}

func TestScopeMismatchDimensionIsDeterministic(t *testing.T) {
	scope := map[string]string{"country": "MX", "supplier": "hb", "agency": "acme"}
	ctx := map[string]any{"country": "US", "supplier": "x", "agency": "other"}
	// Every offending dimension is wrong; the reported one must be the first in
	// sorted order, stably across runs.
	for i := 0; i < 100; i++ {
		if _, mismatch := scopeMatch(scope, ctx); mismatch != "agency" {
			t.Fatalf("mismatch = %q, want stable %q", mismatch, "agency")
		}
	}
}

func TestEvaluateFiltersByValidityAndScope(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	set := loadFixture[RuleSet](t, "ruleset.json")
	program, report := Compile(schema, set)
	if program == nil || !report.Valid() {
		t.Fatalf("fixture did not compile: %+v", report.Issues)
	}

	// HOTELBEDS_MX is valid 2026-06-01..2026-09-01; evaluate after its window.
	at := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	input := Evaluation{
		Layer:       "operation",
		EffectiveAt: at,
		Context:     map[string]any{"country": "US", "supplier": "hotelbeds"},
	}
	result, err := program.Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.RuleSetID != set.ID || result.RuleSetVersion != set.Version || result.Layer != "operation" {
		t.Fatalf("metadata not populated: %+v", result)
	}

	byID := map[string]TraceStatus{}
	for _, entry := range result.Trace.Candidates {
		byID[entry.RuleID] = entry.Status
	}
	if byID["HOTELBEDS_MX"] != TraceNotEffective {
		t.Fatalf("HOTELBEDS_MX should be NOT_EFFECTIVE, got %q", byID["HOTELBEDS_MX"])
	}
	if byID["MEXICO"] != TraceScopeMismatch {
		t.Fatalf("MEXICO should be SCOPE_MISMATCH (country=US), got %q", byID["MEXICO"])
	}
	if result.Status != ResultMatch || result.Winner == nil {
		t.Fatalf("expected a surviving winner, got %+v", result)
	}
}

func TestEvaluateNoMatchWhenAllDiscarded(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	set := loadFixture[RuleSet](t, "ruleset.json")
	program, _ := Compile(schema, set)

	// Unknown layer yields NO_MATCH with an empty, non-nil trace.
	result, err := program.Evaluate(Evaluation{Layer: "does-not-exist", EffectiveAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultNoMatch || result.Winner != nil {
		t.Fatalf("unknown layer must be NO_MATCH, got %+v", result)
	}
	if result.Trace.Candidates == nil {
		t.Fatalf("trace candidates must be non-nil")
	}
}

func TestEvaluateTraceOrderIsStable(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	set := loadFixture[RuleSet](t, "ruleset.json")
	program, _ := Compile(schema, set)
	input := Evaluation{
		Layer:       "operation",
		EffectiveAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Context:     map[string]any{"country": "MX", "supplier": "hotelbeds", "amount_minor": int64(200000), "active": true},
	}
	first, _ := program.Evaluate(input)
	for i := 0; i < 20; i++ {
		next, _ := program.Evaluate(input)
		if !reflect.DeepEqual(first, next) {
			t.Fatalf("evaluation is not deterministic\nfirst: %+v\n next: %+v", first, next)
		}
	}
}

func TestEvalConditionDirect(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	set := loadFixture[RuleSet](t, "ruleset.json")
	program, report := Compile(schema, set)
	if program == nil || !report.Valid() {
		t.Fatalf("fixture did not compile: %+v", report.Issues)
	}

	mexico := compiledByID(t, program, "operation", "MEXICO")         // amount_minor > 100000
	fixed := compiledByID(t, program, "operation", "HOTELBEDS_FIXED") // active
	tiers := compiledByID(t, program, "split", "CONTRACT_TIERS")      // travel_date >= 2026-01-01
	base := compiledByID(t, program, "operation", "BASE_GLOBAL")      // empty -> "true"

	travel := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		prg     cel.Program
		ctx     map[string]any
		want    bool
		wantErr bool
	}{
		{"int true", mexico.program, map[string]any{"amount_minor": int64(200000)}, true, false},
		{"int false", mexico.program, map[string]any{"amount_minor": int64(50000)}, false, false},
		{"bool true", fixed.program, map[string]any{"active": true}, true, false},
		{"bool false", fixed.program, map[string]any{"active": false}, false, false},
		{"timestamp compare", tiers.program, map[string]any{"travel_date": travel}, true, false},
		{"empty condition always true", base.program, map[string]any{}, true, false},
		{"missing variable errors", mexico.program, map[string]any{}, false, true},
		{"wrong type errors", mexico.program, map[string]any{"amount_minor": "not-int"}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evalCondition(tc.prg, tc.ctx)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Fatalf("evalCondition = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEvaluateFiltersByCEL(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	set := loadFixture[RuleSet](t, "ruleset.json")
	program, report := Compile(schema, set)
	if program == nil || !report.Valid() {
		t.Fatalf("fixture did not compile: %+v", report.Issues)
	}

	at := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	// MEXICO condition (amount_minor > 100000) is FALSE here; HOTELBEDS_FIXED
	// references "active", which is absent -> CONDITION_ERROR. HOTELBEDS_MX
	// (scope supplier+country, no condition) is effective and the most specific
	// survivor, so it wins over the global BASE_GLOBAL.
	result, err := program.Evaluate(Evaluation{
		Layer:       "operation",
		EffectiveAt: at,
		Context:     map[string]any{"country": "MX", "supplier": "hotelbeds", "amount_minor": int64(50000)},
	})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]TraceStatus{}
	for _, entry := range result.Trace.Candidates {
		byID[entry.RuleID] = entry.Status
	}
	if byID["MEXICO"] != TraceConditionFalse {
		t.Fatalf("MEXICO should be CONDITION_FALSE, got %q", byID["MEXICO"])
	}
	if byID["HOTELBEDS_FIXED"] != TraceConditionError {
		t.Fatalf("HOTELBEDS_FIXED should be CONDITION_ERROR (active absent), got %q", byID["HOTELBEDS_FIXED"])
	}
	if byID["HOTELBEDS_MX"] != TraceWinner {
		t.Fatalf("HOTELBEDS_MX (supplier+country) should win on specificity, got %q", byID["HOTELBEDS_MX"])
	}
	if byID["BASE_GLOBAL"] != TraceShadowed {
		t.Fatalf("BASE_GLOBAL should be SHADOWED by a more specific rule, got %q", byID["BASE_GLOBAL"])
	}
	if result.Status != ResultMatch || result.Winner == nil || result.Winner.RuleID != "HOTELBEDS_MX" {
		t.Fatalf("expected HOTELBEDS_MX winner, got %+v", result)
	}

	// With supplier absent both HOTELBEDS rules mismatch scope. MEXICO now passes
	// CEL and, scoping country, outranks the global BASE_GLOBAL on specificity.
	result, err = program.Evaluate(Evaluation{
		Layer:       "operation",
		EffectiveAt: at,
		Context:     map[string]any{"country": "MX", "supplier": "none", "amount_minor": int64(200000)},
	})
	if err != nil {
		t.Fatal(err)
	}
	byID = map[string]TraceStatus{}
	for _, entry := range result.Trace.Candidates {
		byID[entry.RuleID] = entry.Status
	}
	if byID["MEXICO"] != TraceWinner {
		t.Fatalf("MEXICO should survive CEL and win on specificity, got %q", byID["MEXICO"])
	}
	if byID["BASE_GLOBAL"] != TraceShadowed {
		t.Fatalf("BASE_GLOBAL should be SHADOWED, got %q", byID["BASE_GLOBAL"])
	}
	if result.Winner == nil || result.Winner.RuleID != "MEXICO" {
		t.Fatalf("expected MEXICO winner, got %+v", result)
	}
}

func TestEvaluateNoCELSurvivorIsNoMatch(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	set := loadFixture[RuleSet](t, "ruleset.json")
	program, _ := Compile(schema, set)

	// split layer: only CONTRACT_TIERS, scope contract=ctr_hb_2026 mismatched.
	result, err := program.Evaluate(Evaluation{
		Layer:       "split",
		EffectiveAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Context:     map[string]any{"contract": "other", "travel_date": time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultNoMatch || result.Winner != nil {
		t.Fatalf("expected NO_MATCH, got %+v", result)
	}
}

// rankingProgram compiles rules over the dimensions country < supplier <
// contract (least → most specific), reusing the fixture schema.
func rankingProgram(t *testing.T, rules ...Rule) *Program {
	t.Helper()
	schema := loadFixture[Schema](t, "schema.json")
	set := RuleSet{
		ID:            "ranking",
		Version:       "v1",
		SchemaVersion: "v1",
		Dimensions:    []string{"country", "supplier", "contract"},
		Rules:         rules,
	}
	program, report := Compile(schema, set)
	if program == nil || !report.Valid() {
		t.Fatalf("ranking fixture did not compile: %+v", report.Issues)
	}
	return program
}

// pctRule builds an unconditional operation rule with a fixed percentage
// outcome so tests can vary only scope and priority.
func pctRule(id string, priority int32, scope map[string]string) Rule {
	return Rule{
		ID:       id,
		Layer:    "operation",
		Scope:    scope,
		Priority: priority,
		Outcome:  Outcome{Kind: OutcomePercentage, Percentage: &PercentageOutcome{Rate: "0.1"}},
	}
}

func TestRankingSelectsMostSpecific(t *testing.T) {
	ctx := map[string]any{"country": "MX", "supplier": "hotelbeds", "contract": "ctr1"}
	cases := []struct {
		name  string
		rules []Rule
		want  string
	}{
		{
			name:  "country beats global",
			rules: []Rule{pctRule("GLOBAL", 0, nil), pctRule("COUNTRY", 0, map[string]string{"country": "MX"})},
			want:  "COUNTRY",
		},
		{
			name:  "supplier beats country",
			rules: []Rule{pctRule("COUNTRY", 0, map[string]string{"country": "MX"}), pctRule("SUPPLIER", 0, map[string]string{"supplier": "hotelbeds"})},
			want:  "SUPPLIER",
		},
		{
			name:  "supplier+country beats supplier",
			rules: []Rule{pctRule("SUPPLIER", 0, map[string]string{"supplier": "hotelbeds"}), pctRule("SUPPLIER_COUNTRY", 0, map[string]string{"supplier": "hotelbeds", "country": "MX"})},
			want:  "SUPPLIER_COUNTRY",
		},
		{
			name:  "contract outranks everything",
			rules: []Rule{pctRule("CONTRACT", 0, map[string]string{"contract": "ctr1"}), pctRule("SUPPLIER_COUNTRY", 0, map[string]string{"supplier": "hotelbeds", "country": "MX"})},
			want:  "CONTRACT",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			program := rankingProgram(t, tc.rules...)
			result, err := program.Evaluate(Evaluation{Layer: "operation", EffectiveAt: time.Now(), Context: ctx})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Status != ResultMatch || result.Winner == nil || result.Winner.RuleID != tc.want {
				t.Fatalf("winner = %+v, want %q", result.Winner, tc.want)
			}
		})
	}
}

func TestRankingPriorityBreaksSpecificityTie(t *testing.T) {
	program := rankingProgram(t,
		pctRule("LOW", 5, map[string]string{"country": "MX"}),
		pctRule("HIGH", 10, map[string]string{"country": "MX"}),
	)
	result, err := program.Evaluate(Evaluation{Layer: "operation", EffectiveAt: time.Now(), Context: map[string]any{"country": "MX"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Winner == nil || result.Winner.RuleID != "HIGH" {
		t.Fatalf("higher priority should win, got %+v", result.Winner)
	}
	byID := map[string]TraceStatus{}
	for _, entry := range result.Trace.Candidates {
		byID[entry.RuleID] = entry.Status
	}
	if byID["LOW"] != TraceShadowed {
		t.Fatalf("LOW should be SHADOWED, got %q", byID["LOW"])
	}
	// Both share the top specificity, so both are recorded as the tie-break set.
	if !reflect.DeepEqual(result.Trace.TieBreakers, []string{"HIGH", "LOW"}) {
		t.Fatalf("tie-breakers should list both rules in id order, got %v", result.Trace.TieBreakers)
	}
}

func TestRankingPriorityNeverOverridesSpecificity(t *testing.T) {
	// COUNTRY carries a far higher priority, but SUPPLIER is more specific and
	// must win: priority only breaks ties within an identical specificity.
	program := rankingProgram(t,
		pctRule("COUNTRY", 100, map[string]string{"country": "MX"}),
		pctRule("SUPPLIER", 0, map[string]string{"supplier": "hotelbeds"}),
	)
	result, err := program.Evaluate(Evaluation{Layer: "operation", EffectiveAt: time.Now(), Context: map[string]any{"country": "MX", "supplier": "hotelbeds"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Winner == nil || result.Winner.RuleID != "SUPPLIER" {
		t.Fatalf("specificity must beat priority, got %+v", result.Winner)
	}
}

func TestEvaluateAmbiguousMatch(t *testing.T) {
	program := rankingProgram(t,
		pctRule("ALPHA", 7, map[string]string{"country": "MX"}),
		pctRule("BETA", 7, map[string]string{"country": "MX"}),
	)
	result, err := program.Evaluate(Evaluation{Layer: "operation", EffectiveAt: time.Now(), Context: map[string]any{"country": "MX"}})

	ambiguous, ok := err.(*AmbiguousMatchError)
	if !ok {
		t.Fatalf("expected *AmbiguousMatchError, got %v (%T)", err, err)
	}
	if result.Status != ResultAmbiguous || result.Winner != nil {
		t.Fatalf("ambiguous result must carry no winner, got %+v", result)
	}
	if ambiguous.Layer != "operation" || !reflect.DeepEqual(ambiguous.RuleIDs, []string{"ALPHA", "BETA"}) {
		t.Fatalf("error should name both tied rules in id order, got %+v", ambiguous)
	}
	byID := map[string]TraceStatus{}
	for _, entry := range result.Trace.Candidates {
		byID[entry.RuleID] = entry.Status
	}
	if byID["ALPHA"] != TraceAmbiguous || byID["BETA"] != TraceAmbiguous {
		t.Fatalf("both rules should be AMBIGUOUS, got %+v", byID)
	}
}

func TestEvaluateResultOutcomeIsIndependent(t *testing.T) {
	schema := loadFixture[Schema](t, "schema.json")
	limit := int64(100000)
	set := RuleSet{
		ID:            "clone",
		Version:       "v1",
		SchemaVersion: "v1",
		Dimensions:    []string{"country"},
		Rules: []Rule{{
			ID:    "TIERED",
			Layer: "operation",
			Scope: map[string]string{"country": "MX"},
			Outcome: Outcome{Kind: OutcomeTiered, Tiered: &TieredOutcome{
				Metric: "amount_minor",
				Tiers:  []Tier{{UpToExclusive: &limit, Rate: "0.7"}, {UpToExclusive: nil, Rate: "0.9"}},
			}},
		}},
	}
	program, report := Compile(schema, set)
	if program == nil || !report.Valid() {
		t.Fatalf("did not compile: %+v", report.Issues)
	}

	input := Evaluation{Layer: "operation", EffectiveAt: time.Now(), Context: map[string]any{"country": "MX"}}
	first, err := program.Evaluate(input)
	if err != nil || first.Winner == nil {
		t.Fatalf("expected winner, got %+v (err %v)", first, err)
	}

	// Mutating the returned outcome must not reach the Program or later results.
	*first.Winner.Outcome.Tiered.Tiers[0].UpToExclusive = 1
	first.Winner.Outcome.Tiered.Tiers[0].Rate = "0.99"

	second, err := program.Evaluate(input)
	if err != nil || second.Winner == nil {
		t.Fatalf("expected winner, got %+v (err %v)", second, err)
	}
	if got := *second.Winner.Outcome.Tiered.Tiers[0].UpToExclusive; got != 100000 {
		t.Fatalf("mutation leaked into Program: tier limit = %d", got)
	}
	if got := second.Winner.Outcome.Tiered.Tiers[0].Rate; got != "0.7" {
		t.Fatalf("mutation leaked into Program: tier rate = %q", got)
	}
}
