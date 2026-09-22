package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"testing"
)

// Opt-in diagnostic: allocation history covers compilation; heap snapshots
// distinguish allocations awaiting GC from objects retained by the Program.
// Run separately from benchmarks because profiling perturbs memory and timing.
func TestCompileMemoryProfile(t *testing.T) {
	directory := os.Getenv("RULEFARE_PROFILE_DIR")
	if directory == "" {
		t.Skip("set RULEFARE_PROFILE_DIR to capture profiles")
	}
	schema := loadFixture[Schema](t, "schema.json")
	var rules RuleSet
	if os.Getenv("RULEFARE_PROFILE_UNIQUE") == "1" {
		rules = benchmarkUniqueRules(10000)
	} else {
		rules = benchmarkRules(10000)
	}
	debug.FreeOSMemory()
	write := func(name, kind string) {
		t.Helper()
		file, err := os.Create(filepath.Join(directory, name+".prof"))
		if err != nil {
			t.Fatal(err)
		}
		err = pprof.Lookup(kind).WriteTo(file, 0)
		closeErr := file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	write("before", "heap")
	program, report := Compile(schema, rules)
	if program == nil {
		t.Fatal(report)
	}
	write("compiled", "heap")
	runtime.GC()
	write("retained", "heap")
	write("allocations", "allocs")
	runtime.KeepAlive(program)
}
