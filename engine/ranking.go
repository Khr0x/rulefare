package engine

// rankedRule pairs a surviving rule with its specificity profile so resolution
// can order candidates without recomputing the vector.
type rankedRule struct {
	rule        Rule
	specificity []bool
}

// dimensionSpecificity reports, for each dimension in order, whether the rule
// constrains it through its scope. The slice is aligned with Program.dimensions
// (least → most specific range), so the last index is the most specific
// dimension.
func dimensionSpecificity(scope map[string]string, dimensions []string) []bool {
	vector := make([]bool, len(dimensions))
	for i, dimension := range dimensions {
		_, constrained := scope[dimension]
		vector[i] = constrained
	}
	return vector
}

// compareSpecificity ranks two aligned specificity vectors lexicographically.
// The most specific dimension (the last in Dimensions) is the most significant
// position, and a constrained dimension outranks an unconstrained one. It
// returns +1 when a is more specific, -1 when b is, and 0 when the vectors are
// identical. Callers must pass vectors of equal length (same dimensions).
func compareSpecificity(a, b []bool) int {
	for i := len(a) - 1; i >= 0; i-- {
		if a[i] == b[i] {
			continue
		}
		if a[i] {
			return 1
		}
		return -1
	}
	return 0
}

// ruleIDs lists the ids of ranked rules in their current order for a stable,
// presentation-only trace; ids never decide the winner.
func ruleIDs(rules []rankedRule) []string {
	ids := make([]string, len(rules))
	for i, r := range rules {
		ids[i] = r.rule.ID
	}
	return ids
}
