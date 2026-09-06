package engine

import (
	"fmt"
	"strings"
)

// AmbiguousMatchError is returned by Evaluate when two or more surviving rules
// tie on both specificity and priority, so no deterministic winner exists. It
// is a blocking authoring error: the ruleset must disambiguate the listed
// rules. Resolution never falls back to load order, map order or rule id, so a
// persistent tie fails closed rather than picking an arbitrary rule.
type AmbiguousMatchError struct {
	Layer   string
	RuleIDs []string
}

func (e *AmbiguousMatchError) Error() string {
	return fmt.Sprintf("ambiguous match in layer %q between rules %s", e.Layer, strings.Join(e.RuleIDs, ", "))
}
