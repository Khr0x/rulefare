package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/cel-go/cel"
)

// Compile validates schema and rules, precompiles CEL, and returns an
// immutable Program. Program is nil whenever the report contains an error.
func Compile(schema Schema, rules RuleSet) (*Program, ValidationReport) {
	issues := validateStructure(schema, rules)
	report := issues.report()
	if !report.Valid() {
		return nil, report
	}

	env, err := newCELEnvironment(schema)
	if err != nil {
		issues.add(IssueInvalidCEL, "/schema", "CEL environment could not be created")
		return nil, issues.report()
	}

	layers := make(map[string][]compiledRule)
	for i, sourceRule := range rules.Rules {
		expression := sourceRule.Condition
		if strings.TrimSpace(expression) == "" {
			expression = "true"
		}
		ast, compileIssues := env.Compile(expression)
		path := fmt.Sprintf("/rules/%d/condition", i)
		if compileIssues != nil && compileIssues.Err() != nil {
			issues.add(IssueInvalidCEL, path, "condition is not valid CEL")
			continue
		}
		if ast.OutputType().Kind() != cel.BoolKind {
			issues.add(IssueCELNotBool, path, "condition must return bool")
			continue
		}
		celProgram, programErr := env.Program(ast)
		if programErr != nil {
			issues.add(IssueInvalidCEL, path, "condition could not be compiled")
			continue
		}
		rule := cloneRule(sourceRule)
		if rule.Composition == "" {
			rule.Composition = CompositionFirstMatch
		}
		layers[rule.Layer] = append(layers[rule.Layer], compiledRule{
			rule:    rule,
			program: celProgram,
		})
	}

	report = issues.report()
	if !report.Valid() {
		return nil, report
	}
	for layer := range layers {
		sort.Slice(layers[layer], func(i, j int) bool {
			return layers[layer][i].rule.ID < layers[layer][j].rule.ID
		})
	}

	return &Program{
		schema:        cloneSchema(schema),
		ruleSetID:     rules.ID,
		version:       rules.Version,
		schemaVersion: rules.SchemaVersion,
		dimensions:    append([]string(nil), rules.Dimensions...),
		layers:        layers,
	}, report
}

func newCELEnvironment(schema Schema) (*cel.Env, error) {
	options := []cel.EnvOption{
		cel.ClearMacros(),
		cel.ParserExpressionSizeLimit(maxConditionBytes),
		cel.ExpressionNodeLimit(256),
		cel.ExpressionNestingDepthLimit(32),
		cel.ParserRecursionLimit(32),
	}
	for _, name := range sortedKeys(schema.Variables) {
		valueType, ok := celType(schema.Variables[name])
		if !ok {
			return nil, fmt.Errorf("unsupported CEL type")
		}
		options = append(options, cel.Variable(name, valueType))
	}
	return cel.NewEnv(options...)
}

func celType(valueType ValueType) (*cel.Type, bool) {
	switch valueType {
	case ValueBool:
		return cel.BoolType, true
	case ValueInt:
		return cel.IntType, true
	case ValueString:
		return cel.StringType, true
	case ValueTimestamp:
		return cel.TimestampType, true
	default:
		return nil, false
	}
}

func cloneSchema(schema Schema) Schema {
	variables := make(map[string]ValueType, len(schema.Variables))
	for key, value := range schema.Variables {
		variables[key] = value
	}
	return Schema{Version: schema.Version, Variables: variables}
}

func cloneRule(rule Rule) Rule {
	clone := rule
	clone.Scope = make(map[string]string, len(rule.Scope))
	for key, value := range rule.Scope {
		clone.Scope[key] = value
	}
	if rule.ValidFrom != nil {
		value := *rule.ValidFrom
		clone.ValidFrom = &value
	}
	if rule.ValidTo != nil {
		value := *rule.ValidTo
		clone.ValidTo = &value
	}
	clone.Outcome = cloneOutcome(rule.Outcome)
	return clone
}

func cloneOutcome(outcome Outcome) Outcome {
	clone := Outcome{Kind: outcome.Kind}
	if outcome.Percentage != nil {
		value := *outcome.Percentage
		clone.Percentage = &value
	}
	if outcome.Fixed != nil {
		value := *outcome.Fixed
		clone.Fixed = &value
	}
	if outcome.Tiered != nil {
		value := TieredOutcome{Metric: outcome.Tiered.Metric}
		value.Tiers = make([]Tier, len(outcome.Tiered.Tiers))
		for i, tier := range outcome.Tiered.Tiers {
			value.Tiers[i] = tier
			if tier.UpToExclusive != nil {
				limit := *tier.UpToExclusive
				value.Tiers[i].UpToExclusive = &limit
			}
		}
		clone.Tiered = &value
	}
	return clone
}
