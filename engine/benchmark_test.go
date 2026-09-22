package engine

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func benchmarkRules(count int) RuleSet {
	rules := minimalRuleSet("")
	rules.Dimensions = []string{"country", "supplier"}
	rules.Rules = make([]Rule, count)
	for i := range rules.Rules {
		rules.Rules[i] = Rule{
			ID: fmt.Sprintf("RULE_%05d", i), Layer: "operation", Priority: int32(i),
			Scope:     map[string]string{"country": "MX", "supplier": "hotelbeds"},
			Condition: "active && amount_minor >= 100000 && travel_date >= timestamp('2026-01-01T00:00:00Z')",
			Outcome:   Outcome{Kind: OutcomePercentage, Percentage: &PercentageOutcome{Rate: "0.17"}},
		}
	}
	return rules
}

func benchmarkUniqueRules(count int) RuleSet {
	rules := benchmarkRules(count)
	for i := range rules.Rules {
		rules.Rules[i].Condition = fmt.Sprintf("active && amount_minor >= %d && travel_date >= timestamp('2026-01-01T00:00:00Z')", i)
	}
	return rules
}

func benchmarkEvaluation() Evaluation {
	return Evaluation{
		Layer: "operation", EffectiveAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Context: fixtureContext(map[string]any{
			"country": "MX", "supplier": "hotelbeds", "amount_minor": int64(200000), "active": true,
		}),
	}
}

func benchmarkProgram(b *testing.B, count int) *Program {
	b.Helper()
	program, report := Compile(loadFixture[Schema](b, "schema.json"), benchmarkRules(count))
	if program == nil {
		b.Fatal(report)
	}
	return program
}

func BenchmarkEvaluate(b *testing.B) {
	for _, count := range []int{10, 100, 10000} {
		b.Run(strconv.Itoa(count), func(b *testing.B) { benchmarkEvaluate(b, benchmarkRules(count)) })
	}
}

// Control for the per-call condition reuse: every expression is distinct.
func BenchmarkEvaluateUnique(b *testing.B) {
	for _, count := range []int{10, 100, 10000} {
		b.Run(strconv.Itoa(count), func(b *testing.B) { benchmarkEvaluate(b, benchmarkUniqueRules(count)) })
	}
}

func benchmarkEvaluate(b *testing.B, rules RuleSet) {
	program, report := Compile(loadFixture[Schema](b, "schema.json"), rules)
	if program == nil {
		b.Fatal(report)
	}
	count := len(rules.Rules)
	input := benchmarkEvaluation()
	want := fmt.Sprintf("RULE_%05d", count-1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := program.Evaluate(input)
		if err != nil || result.Winner == nil || result.Winner.RuleID != want || len(result.Trace.Candidates) != count {
			b.Fatalf("unexpected result: %+v, %v", result, err)
		}
	}
}

// Run separately with -benchtime=10000x: clock reads and percentile collection
// are kept out of the throughput/allocation benchmark above.
func BenchmarkEvaluateLatency100(b *testing.B) {
	program := benchmarkProgram(b, 100)
	input := benchmarkEvaluation()
	samples := make([]time.Duration, min(b.N, 10000))
	for i := 0; i < 100; i++ {
		if _, err := program.Evaluate(input); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		result, err := program.Evaluate(input)
		samples[i%len(samples)] = time.Since(start)
		if err != nil || result.Winner == nil || result.Winner.RuleID != "RULE_00099" {
			b.Fatalf("unexpected result: %+v, %v", result, err)
		}
	}
	b.StopTimer()
	slices.Sort(samples)
	// Nearest-rank percentiles of the last (at most 10000) observations.
	b.ReportMetric(float64(samples[(95*len(samples)+99)/100-1].Nanoseconds()), "p95-ns")
	b.ReportMetric(float64(samples[(99*len(samples)+99)/100-1].Nanoseconds()), "p99-ns")
	b.ReportMetric(float64(len(samples)), "samples")
}

func BenchmarkCompile10000(b *testing.B) {
	benchmarkCompile(b, benchmarkRules(10000))
}

func BenchmarkCompileUnique10000(b *testing.B) {
	benchmarkCompile(b, benchmarkUniqueRules(10000))
}

func benchmarkCompile(b *testing.B, rules RuleSet) {
	schema := loadFixture[Schema](b, "schema.json")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		program, report := Compile(schema, rules)
		if program == nil {
			b.Fatal(report)
		}
		runtime.KeepAlive(program)
	}
}

// Run in its own process with -benchtime=1x. RSS is absolute process residency,
// while retained heap is the delta from before constructing the Program.
func BenchmarkProgramResident10000(b *testing.B) {
	benchmarkResident(b, false)
}

// Control: distinct expressions cannot benefit from program reuse.
func BenchmarkProgramResidentUnique10000(b *testing.B) {
	benchmarkResident(b, true)
}

func benchmarkResident(b *testing.B, unique bool) {
	for i := 0; i < b.N; i++ {
		debug.FreeOSMemory()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		var rules RuleSet
		if unique {
			rules = benchmarkUniqueRules(10000)
		} else {
			rules = benchmarkRules(10000)
		}
		program, report := Compile(loadFixture[Schema](b, "schema.json"), rules)
		if program == nil {
			b.Fatal(report)
		}
		runtime.GC()
		runtime.ReadMemStats(&after)
		rssAfterGC := benchmarkRSS(b)
		debug.FreeOSMemory()
		rssReleased := benchmarkRSS(b)
		runtime.KeepAlive(program)
		b.ReportMetric(float64(int64(after.HeapAlloc)-int64(before.HeapAlloc)), "retained-heap-B")
		b.ReportMetric(float64(rssAfterGC), "rss-after-gc-B")
		b.ReportMetric(float64(rssReleased), "rss-released-B")
	}
}

func benchmarkRSS(b *testing.B) int64 {
	b.Helper()
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile("/proc/self/statm")
		if err != nil {
			b.Fatal(err)
		}
		fields := strings.Fields(string(data))
		if len(fields) < 2 {
			b.Fatal("invalid /proc/self/statm")
		}
		pages, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			b.Fatal(err)
		}
		return pages * int64(os.Getpagesize())
	}
	// ps reports KiB on both supported measurement hosts (Linux and macOS).
	output, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		b.Fatalf("cannot measure current RSS: %v", err)
	}
	kib, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		b.Fatal(err)
	}
	return kib * 1024
}

// Adversarial values are constructed outside the timer. This measures the
// engine's rejection/evaluation cost, not allocating attacker-owned input.
func BenchmarkResourceLimits(b *testing.B) {
	for _, scenario := range []string{"string_boundary", "oversized_context", "condition_cost", "regex_cost", "regex_boundary", "aggregate_cost"} {
		b.Run(scenario, func(b *testing.B) {
			rules := minimalRuleSet("true")
			input := benchmarkEvaluation()
			input.Context["country"] = strings.Repeat("a", maxStringBytes)
			want := ResultMatch
			switch scenario {
			case "oversized_context":
				input.Context["country"] = strings.Repeat("a", 1<<20)
				want = ResultInvalidInput
			case "regex_boundary":
				rules.Rules[0].Condition = "country.matches(supplier)"
				input.Context["country"] = strings.Repeat("a", maxRegexBytes)
				input.Context["supplier"] = strings.Repeat("a", maxRegexBytes)
			case "condition_cost", "regex_cost":
				rules.Rules[0].Condition = "country.contains(supplier)"
				if scenario == "regex_cost" {
					rules.Rules[0].Condition = "country.matches(supplier)"
				}
				input.Context["supplier"] = strings.Repeat("a", maxStringBytes)
				want = ResultLimitExceeded
			case "aggregate_cost":
				rules = benchmarkRules(3000)
				for i := range rules.Rules {
					rules.Rules[i].Scope = nil
					rules.Rules[i].Condition = "country.contains(supplier)"
				}
				input.Context["supplier"] = "a"
				want = ResultLimitExceeded
			}
			program, report := Compile(loadFixture[Schema](b, "schema.json"), rules)
			if program == nil {
				b.Fatal(report)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := program.Evaluate(input)
				if result.Status != want || (err == nil) != (want == ResultMatch) {
					b.Fatalf("%+v %v", result, err)
				}
			}
		})
	}
	b.Run("oversized_decimal", func(b *testing.B) {
		schema := loadFixture[Schema](b, "schema.json")
		rules := minimalRuleSet("true")
		rules.Rules[0].Outcome.Percentage.Rate = strings.Repeat("9", 1<<20)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			program, report := Compile(schema, rules)
			if program != nil || len(report.Issues) != 1 || report.Issues[0].Code != IssueLimitExceeded {
				b.Fatal(report)
			}
		}
	})
}
