package rulefare_test

import (
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const module = "github.com/Khr0x/rulefare"

// importRule restricts the direct imports of one package. Standard-library
// imports are always allowed; anything else must match an allowed prefix.
type importRule struct {
	pkg     string
	allowed []string
}

// rules is the dependency boundary from the F2 plan: the engine stays
// independent of the platform, and money stays a leaf package.
var rules = []importRule{
	{pkg: module + "/engine", allowed: []string{"github.com/google/cel-go/"}},
	{pkg: module + "/internal/money"},
}

type listedPackage struct {
	ImportPath string
	Imports    []string
	Standard   bool
}

func TestDependencyBoundaries(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-json", "./...").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("go list: %v\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("go list: %v", err)
	}
	packages := map[string]listedPackage{}
	decoder := json.NewDecoder(strings.NewReader(string(out)))
	for {
		var p listedPackage
		if err := decoder.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		packages[p.ImportPath] = p
	}
	if _, ok := packages[module+"/engine"]; !ok {
		t.Fatal("engine package not found; the boundary check would pass vacuously")
	}
	for _, v := range violations(packages, rules) {
		t.Error(v)
	}
}

func TestDependencyBoundariesDetectViolations(t *testing.T) {
	packages := map[string]listedPackage{
		module + "/engine": {ImportPath: module + "/engine", Imports: []string{
			"fmt", "github.com/google/cel-go/cel", module + "/internal/postgres", "github.com/jackc/pgx/v5",
		}},
		"fmt":                          {ImportPath: "fmt", Standard: true},
		"github.com/google/cel-go/cel": {ImportPath: "github.com/google/cel-go/cel"},
		module + "/internal/postgres":  {ImportPath: module + "/internal/postgres"},
		"github.com/jackc/pgx/v5":      {ImportPath: "github.com/jackc/pgx/v5"},
	}
	got := violations(packages, rules)
	want := []string{
		module + "/engine imports forbidden package " + module + "/internal/postgres",
		module + "/engine imports forbidden package github.com/jackc/pgx/v5",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
}

func violations(packages map[string]listedPackage, rules []importRule) []string {
	var found []string
	for _, rule := range rules {
		p, ok := packages[rule.pkg]
		if !ok {
			continue // Package not written yet; the rule applies once it exists.
		}
		for _, dep := range p.Imports {
			if packages[dep].Standard || allowed(dep, rule.allowed) {
				continue
			}
			found = append(found, rule.pkg+" imports forbidden package "+dep)
		}
	}
	return found
}

func allowed(dep string, prefixes []string) bool {
	if dep == module || strings.HasPrefix(dep, module+"/") {
		return false // Module packages are never allowed by prefix.
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(dep, prefix) {
			return true
		}
	}
	return false
}
