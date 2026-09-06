package engine

import (
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
)

const (
	maxRules            = 10_000
	maxDimensions       = 32
	maxVariables        = 256
	maxConditionBytes   = 4_096
	maxValidationIssues = 1_000
	maxTiers            = 100
)

var (
	idPattern       = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._:-]{0,127}$`)
	namePattern     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	decimalPattern  = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]*[1-9])?$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

type issueCollector struct {
	issues    []ValidationIssue
	truncated bool
}

func (c *issueCollector) add(code IssueCode, path, message string) {
	if c.truncated {
		return
	}
	if len(c.issues) >= maxValidationIssues-1 {
		c.issues = append(c.issues, ValidationIssue{
			Code:     IssueLimitExceeded,
			Path:     "",
			Message:  "validation stopped after reaching the issue limit",
			Severity: SeverityError,
		})
		c.truncated = true
		return
	}
	c.issues = append(c.issues, ValidationIssue{
		Code:     code,
		Path:     path,
		Message:  message,
		Severity: SeverityError,
	})
}

func (c *issueCollector) report() ValidationReport {
	sort.SliceStable(c.issues, func(i, j int) bool {
		if c.issues[i].Path != c.issues[j].Path {
			return c.issues[i].Path < c.issues[j].Path
		}
		return c.issues[i].Code < c.issues[j].Code
	})
	return ValidationReport{Issues: append([]ValidationIssue{}, c.issues...)}
}

func validateStructure(schema Schema, set RuleSet) *issueCollector {
	issues := &issueCollector{}

	if strings.TrimSpace(schema.Version) == "" {
		issues.add(IssueRequired, "/schema/version", "schema version is required")
	}
	if len(schema.Variables) > maxVariables {
		issues.add(IssueLimitExceeded, "/schema/variables", fmt.Sprintf("at most %d variables are allowed", maxVariables))
	}
	variableNames := sortedKeys(schema.Variables)
	for _, name := range variableNames {
		path := "/schema/variables/" + escapeJSONPointer(name)
		if !namePattern.MatchString(name) {
			issues.add(IssueInvalidRange, path, "variable name is invalid")
		}
		if !validValueType(schema.Variables[name]) {
			issues.add(IssueUnknownType, path, "variable type is unsupported")
		}
	}

	if !idPattern.MatchString(set.ID) {
		code := IssueInvalidRange
		if set.ID == "" {
			code = IssueRequired
		}
		issues.add(code, "/id", "ruleset id is required and must use the supported format")
	}
	if strings.TrimSpace(set.Version) == "" {
		issues.add(IssueRequired, "/version", "ruleset version is required")
	}
	if strings.TrimSpace(set.SchemaVersion) == "" {
		issues.add(IssueRequired, "/schema_version", "schema version is required")
	} else if schema.Version != "" && set.SchemaVersion != schema.Version {
		issues.add(IssueSchemaMismatch, "/schema_version", "ruleset schema version does not match schema")
	}
	if len(set.Dimensions) > maxDimensions {
		issues.add(IssueLimitExceeded, "/dimensions", fmt.Sprintf("at most %d dimensions are allowed", maxDimensions))
	}
	dimensions := make(map[string]struct{}, len(set.Dimensions))
	for i, dimension := range set.Dimensions {
		path := fmt.Sprintf("/dimensions/%d", i)
		if !namePattern.MatchString(dimension) {
			issues.add(IssueInvalidRange, path, "dimension name is invalid")
		}
		if _, exists := dimensions[dimension]; exists {
			issues.add(IssueDuplicate, path, "dimension is duplicated")
		}
		dimensions[dimension] = struct{}{}
		valueType, exists := schema.Variables[dimension]
		if !exists {
			issues.add(IssueUnknownDimension, path, "dimension is not declared in schema")
		} else if valueType != ValueString {
			issues.add(IssueUnknownType, path, "dimension variables must use string type")
		}
	}

	if len(set.Rules) > maxRules {
		issues.add(IssueLimitExceeded, "/rules", fmt.Sprintf("at most %d rules are allowed", maxRules))
	}
	ruleIDs := make(map[string]struct{}, len(set.Rules))
	for i, rule := range set.Rules {
		validateRule(issues, schema, dimensions, ruleIDs, rule, i)
	}

	return issues
}

func validateRule(issues *issueCollector, schema Schema, dimensions, ruleIDs map[string]struct{}, rule Rule, index int) {
	base := fmt.Sprintf("/rules/%d", index)
	if !idPattern.MatchString(rule.ID) {
		code := IssueInvalidRange
		if rule.ID == "" {
			code = IssueRequired
		}
		issues.add(code, base+"/id", "rule id is required and must use the supported format")
	}
	if _, exists := ruleIDs[rule.ID]; exists && rule.ID != "" {
		issues.add(IssueDuplicate, base+"/id", "rule id is duplicated")
	}
	ruleIDs[rule.ID] = struct{}{}
	if strings.TrimSpace(rule.Layer) == "" {
		issues.add(IssueRequired, base+"/layer", "rule layer is required")
	}
	if rule.Composition != "" && rule.Composition != CompositionFirstMatch {
		issues.add(IssueUnsupportedComposition, base+"/composition", "only FIRST_MATCH is supported")
	}
	if rule.ValidFrom != nil && rule.ValidTo != nil && !rule.ValidTo.After(*rule.ValidFrom) {
		issues.add(IssueInvalidRange, base+"/valid_to", "valid_to must be after valid_from")
	}
	if len(rule.Condition) > maxConditionBytes {
		issues.add(IssueLimitExceeded, base+"/condition", fmt.Sprintf("condition must not exceed %d bytes", maxConditionBytes))
	}

	for _, dimension := range sortedKeys(rule.Scope) {
		path := base + "/scope/" + escapeJSONPointer(dimension)
		if _, exists := dimensions[dimension]; !exists {
			issues.add(IssueUnknownDimension, path, "scope uses an unknown dimension")
			continue
		}
		if schema.Variables[dimension] != ValueString {
			issues.add(IssueUnknownType, path, "scope dimensions must use string type")
		}
	}

	validateOutcome(issues, schema, rule.Outcome, base+"/outcome")
}

func validateOutcome(issues *issueCollector, schema Schema, outcome Outcome, path string) {
	payloads := 0
	if outcome.Percentage != nil {
		payloads++
	}
	if outcome.Fixed != nil {
		payloads++
	}
	if outcome.Tiered != nil {
		payloads++
	}
	if payloads != 1 {
		issues.add(IssueInvalidOutcome, path, "outcome must contain exactly one payload")
	}

	switch outcome.Kind {
	case OutcomePercentage:
		if outcome.Percentage == nil || outcome.Fixed != nil || outcome.Tiered != nil {
			issues.add(IssueInvalidOutcome, path, "percentage kind must contain only percentage payload")
			return
		}
		validateRate(issues, outcome.Percentage.Rate, path+"/percentage/rate")
	case OutcomeFixed:
		if outcome.Fixed == nil || outcome.Percentage != nil || outcome.Tiered != nil {
			issues.add(IssueInvalidOutcome, path, "fixed kind must contain only fixed payload")
			return
		}
		if !canonicalDecimal(outcome.Fixed.Amount) {
			issues.add(IssueInvalidOutcome, path+"/fixed/amount", "amount must be a non-negative canonical decimal")
		}
		if !currencyPattern.MatchString(outcome.Fixed.Currency) {
			issues.add(IssueInvalidOutcome, path+"/fixed/currency", "currency must contain three uppercase letters")
		}
	case OutcomeTiered:
		if outcome.Tiered == nil || outcome.Percentage != nil || outcome.Fixed != nil {
			issues.add(IssueInvalidOutcome, path, "tiered kind must contain only tiered payload")
			return
		}
		validateTiered(issues, schema, *outcome.Tiered, path+"/tiered")
	default:
		issues.add(IssueInvalidOutcome, path+"/kind", "outcome kind is unsupported")
	}
}

func validateTiered(issues *issueCollector, schema Schema, tiered TieredOutcome, path string) {
	valueType, exists := schema.Variables[tiered.Metric]
	if tiered.Metric == "" {
		issues.add(IssueRequired, path+"/metric", "tier metric is required")
	} else if !exists || valueType != ValueInt {
		issues.add(IssueUnknownType, path+"/metric", "tier metric must reference an int variable")
	}
	if len(tiered.Tiers) == 0 {
		issues.add(IssueRequired, path+"/tiers", "at least one tier is required")
		return
	}
	if len(tiered.Tiers) > maxTiers {
		issues.add(IssueLimitExceeded, path+"/tiers", fmt.Sprintf("at most %d tiers are allowed", maxTiers))
	}

	var previous int64
	havePrevious := false
	for i, tier := range tiered.Tiers {
		tierPath := fmt.Sprintf("%s/tiers/%d", path, i)
		validateRate(issues, tier.Rate, tierPath+"/rate")
		last := i == len(tiered.Tiers)-1
		if tier.UpToExclusive == nil {
			if !last {
				issues.add(IssueInvalidRange, tierPath+"/up_to_exclusive", "only the final tier may be unbounded")
			}
			continue
		}
		if last {
			issues.add(IssueInvalidRange, tierPath+"/up_to_exclusive", "the final tier must be unbounded")
		}
		if havePrevious && *tier.UpToExclusive <= previous {
			issues.add(IssueInvalidRange, tierPath+"/up_to_exclusive", "tier limits must be strictly increasing")
		}
		previous = *tier.UpToExclusive
		havePrevious = true
	}
}

func validateRate(issues *issueCollector, rate, path string) {
	if !canonicalDecimal(rate) {
		issues.add(IssueInvalidOutcome, path, "rate must be a canonical decimal between 0 and 1")
		return
	}
	value, ok := new(big.Rat).SetString(rate)
	if !ok || value.Sign() < 0 || value.Cmp(big.NewRat(1, 1)) > 0 {
		issues.add(IssueInvalidOutcome, path, "rate must be between 0 and 1")
	}
}

func canonicalDecimal(value string) bool {
	if !decimalPattern.MatchString(value) {
		return false
	}
	_, ok := new(big.Rat).SetString(value)
	return ok
}

func validValueType(valueType ValueType) bool {
	switch valueType {
	case ValueBool, ValueInt, ValueString, ValueTimestamp:
		return true
	default:
		return false
	}
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func escapeJSONPointer(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}
