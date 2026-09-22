package engine

import (
	"encoding/json"
	"math"
	"strings"
	"time"
)

func (p *Program) normalizeEvaluation(input Evaluation) (Evaluation, error) {
	issues := &issueCollector{}
	if len(input.Layer) > maxMetadataBytes {
		issues.add(IssueLimitExceeded, "/layer", "layer exceeds size limit")
		return Evaluation{}, &InvalidEvaluationError{Report: issues.report()}
	}
	if !evaluationWithinLimits(input) {
		issues.add(IssueLimitExceeded, "/context", "evaluation input exceeds size limits")
		return Evaluation{}, &InvalidEvaluationError{Report: issues.report()}
	}
	if strings.TrimSpace(input.Layer) == "" {
		issues.add(IssueRequired, "/layer", "layer is required")
	} else if _, exists := p.layers[input.Layer]; !exists {
		issues.add(IssueSchemaMismatch, "/layer", "layer is not present in the compiled ruleset")
	}
	if input.EffectiveAt.IsZero() {
		issues.add(IssueRequired, "/effective_at", "effective_at is required")
	} else if at, ok := normalizeTimestamp(input.EffectiveAt); ok {
		input.EffectiveAt = at
	} else {
		issues.add(IssueInvalidRange, "/effective_at", "effective_at must be within years 1 through 9999 in UTC")
	}

	context := make(map[string]any, len(p.schema.Variables))
	if input.Context == nil {
		issues.add(IssueRequired, "/context", "context is required")
	}
	for _, name := range sortedKeys(p.schema.Variables) {
		path := "/context/" + escapeJSONPointer(name)
		value, exists := input.Context[name]
		if !exists || value == nil {
			issues.add(IssueRequired, path, "schema variable is required and cannot be null")
			continue
		}
		normalized, ok := normalizeValue(value, p.schema.Variables[name])
		if !ok {
			issues.add(IssueSchemaMismatch, path, "value must match schema type "+string(p.schema.Variables[name]))
			continue
		}
		context[name] = normalized
	}
	for _, name := range sortedKeys(input.Context) {
		if _, exists := p.schema.Variables[name]; !exists {
			issues.add(IssueSchemaMismatch, "/context/"+escapeJSONPointer(name), "variable is not declared in schema")
		}
	}
	if report := issues.report(); !report.Valid() {
		return Evaluation{}, &InvalidEvaluationError{Report: report}
	}
	input.Context = context
	return input, nil
}

func normalizeValue(value any, kind ValueType) (any, bool) {
	switch kind {
	case ValueBool:
		v, ok := value.(bool)
		return v, ok
	case ValueString:
		v, ok := value.(string)
		return v, ok
	case ValueInt:
		return normalizeInteger(value)
	case ValueTimestamp:
		return normalizeTimestamp(value)
	default:
		return nil, false
	}
}

// normalizeInteger accepts native integers, exact JSON integer literals, and
// integral float64 values within JSON's interoperable safe-integer range.
// Larger JSON integers must be decoded with Decoder.UseNumber to avoid rounding.
func normalizeInteger(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint:
		return int64(v), uint64(v) <= math.MaxInt64
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		return int64(v), v <= math.MaxInt64
	case json.Number:
		if len(v) > 20 {
			return 0, false
		}
		n, err := v.Int64()
		return n, err == nil && json.Valid([]byte(v))
	case float64:
		const maxSafeInteger = 1<<53 - 1
		if v >= -maxSafeInteger && v <= maxSafeInteger && math.Trunc(v) == v {
			return int64(v), true
		}
	}
	return 0, false
}

func normalizeTimestamp(value any) (time.Time, bool) {
	var timestamp time.Time
	switch v := value.(type) {
	case time.Time:
		timestamp = v
	case string:
		if len(v) > 35 {
			return time.Time{}, false
		}
		var err error
		timestamp, err = time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return time.Time{}, false
		}
	default:
		return time.Time{}, false
	}
	timestamp = timestamp.UTC()
	return timestamp, !timestamp.IsZero() && timestamp.Year() >= 1 && timestamp.Year() <= 9999
}
