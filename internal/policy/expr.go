package policy

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ExprEvaluator evaluates simple CEL-like expressions against a SessionContext.
// Supports: field comparisons (>, <, >=, <=, ==, !=), AND, OR operators.
// Example: "session.cost > 5.00 AND session.status == 'running'"
type ExprEvaluator struct {
	expr string
}

// NewExprEvaluator creates a new expression evaluator.
func NewExprEvaluator(expr string) *ExprEvaluator {
	return &ExprEvaluator{expr: strings.TrimSpace(expr)}
}

// Evaluate evaluates the expression against the session context.
// Returns the final boolean result and any errors.
func (e *ExprEvaluator) Evaluate(ctx *SessionContext) (bool, error) {
	if e.expr == "" {
		return false, fmt.Errorf("empty expression")
	}

	// Handle AND/OR operators by splitting and recursively evaluating
	// Priority: OR has lower precedence than AND
	result, err := e.evalOr(e.expr, ctx)
	return result, err
}

// evalOr handles OR operator (lowest precedence)
func (e *ExprEvaluator) evalOr(expr string, ctx *SessionContext) (bool, error) {
	parts := splitByOperator(expr, " OR ")
	if len(parts) == 1 {
		// No OR, try AND
		return e.evalAnd(parts[0], ctx)
	}

	for _, part := range parts {
		result, err := e.evalAnd(strings.TrimSpace(part), ctx)
		if err != nil {
			return false, err
		}
		if result {
			return true, nil // Short-circuit: any OR is true
		}
	}
	return false, nil
}

// evalAnd handles AND operator (higher precedence than OR)
func (e *ExprEvaluator) evalAnd(expr string, ctx *SessionContext) (bool, error) {
	parts := splitByOperator(expr, " AND ")
	if len(parts) == 1 {
		// No AND, evaluate the comparison
		return e.evalComparison(parts[0], ctx)
	}

	for _, part := range parts {
		result, err := e.evalComparison(strings.TrimSpace(part), ctx)
		if err != nil {
			return false, err
		}
		if !result {
			return false, nil // Short-circuit: any AND is false
		}
	}
	return true, nil
}

// evalComparison evaluates a single comparison like "session.cost > 5.00"
func (e *ExprEvaluator) evalComparison(expr string, ctx *SessionContext) (bool, error) {
	expr = strings.TrimSpace(expr)

	// Try each operator (longer ones first to avoid partial matches)
	for _, op := range []string{">=", "<=", "==", "!=", ">", "<"} {
		parts := strings.Split(expr, op)
		if len(parts) == 2 {
			left := strings.TrimSpace(parts[0])
			right := strings.TrimSpace(parts[1])

			leftVal, err := e.getFieldValue(left, ctx)
			if err != nil {
				return false, err
			}

			rightVal, err := e.parseValue(right)
			if err != nil {
				return false, err
			}

			return e.compare(leftVal, rightVal, op), nil
		}
	}

	return false, fmt.Errorf("invalid comparison: %s", expr)
}

// getFieldValue retrieves the value of a field from SessionContext.
// Supports: session.cost, session.status, session.errors_last_10min, etc.
func (e *ExprEvaluator) getFieldValue(field string, ctx *SessionContext) (interface{}, error) {
	if !strings.HasPrefix(field, "session.") {
		return nil, fmt.Errorf("unsupported field: %s (only session.* supported)", field)
	}

	fieldName := strings.TrimPrefix(field, "session.")
	switch fieldName {
	case "cost":
		return ctx.Cost, nil
	case "cost_last_hour":
		return ctx.CostLastHour, nil
	case "errors_last_10min":
		return ctx.ErrorsLast10Min, nil
	case "heartbeat_age_sec":
		return ctx.HeartbeatAgeSec, nil
	case "status":
		return ctx.Status, nil
	case "runtime_type":
		return ctx.RuntimeType, nil
	default:
		return nil, fmt.Errorf("unknown field: session.%s", fieldName)
	}
}

// parseValue converts a string value to int, float, or string.
func (e *ExprEvaluator) parseValue(val string) (interface{}, error) {
	val = strings.TrimSpace(val)

	// String literal (quoted)
	if (strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'")) ||
		(strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"")) {
		return strings.Trim(val, "'\""), nil
	}

	// Try integer
	if iv, err := strconv.ParseInt(val, 10, 64); err == nil {
		return int(iv), nil
	}

	// Try float
	if fv, err := strconv.ParseFloat(val, 64); err == nil {
		return fv, nil
	}

	return nil, fmt.Errorf("cannot parse value: %s", val)
}

// compare performs a comparison between two values using the given operator.
func (e *ExprEvaluator) compare(left, right interface{}, op string) bool {
	// Handle string comparisons
	if ls, ok := left.(string); ok {
		if rs, ok := right.(string); ok {
			switch op {
			case "==":
				return ls == rs
			case "!=":
				return ls != rs
			}
			return false // >, <, >=, <= not supported for strings
		}
		return false
	}

	// Coerce to float for numeric comparisons
	lf, lOk := e.toFloat(left)
	rf, rOk := e.toFloat(right)
	if !lOk || !rOk {
		return false
	}

	switch op {
	case ">":
		return lf > rf
	case "<":
		return lf < rf
	case ">=":
		return lf >= rf
	case "<=":
		return lf <= rf
	case "==":
		return lf == rf
	case "!=":
		return lf != rf
	}
	return false
}

// toFloat converts an interface{} to float64.
func (e *ExprEvaluator) toFloat(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	default:
		return 0, false
	}
}

// splitByOperator splits a string by an operator, but respects quotes.
// Returns the parts split by the operator.
func splitByOperator(expr, op string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	i := 0
	for i < len(expr) {
		// Check for quotes
		if (expr[i] == '\'' || expr[i] == '"') && (i == 0 || expr[i-1] != '\\') {
			if !inQuote {
				inQuote = true
				quoteChar = rune(expr[i])
			} else if rune(expr[i]) == quoteChar {
				inQuote = false
				quoteChar = 0
			}
			current.WriteByte(expr[i])
			i++
			continue
		}

		// Check for operator
		if !inQuote && i+len(op) <= len(expr) && expr[i:i+len(op)] == op {
			parts = append(parts, current.String())
			current.Reset()
			i += len(op)
			continue
		}

		current.WriteByte(expr[i])
		i++
	}

	parts = append(parts, current.String())
	return parts
}

// ExtractFirstField extracts the main field being compared in the expression.
// Used for metric tracking/debugging. Example: "session.cost > 5" -> "session.cost"
func ExtractFirstField(expr string) string {
	// Find the first field pattern (session.*)
	re := regexp.MustCompile(`session\.\w+`)
	matches := re.FindStringSubmatch(expr)
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}
