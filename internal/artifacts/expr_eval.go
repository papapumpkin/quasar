package artifacts

import (
	"fmt"
	"strconv"
	"strings"
)

// truthy applies the expression language's truthiness rules: nil and the zero
// value of each scalar type are false, everything else is true.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	default:
		return true
	}
}

// toFloat coerces numeric values to float64 for comparison and arithmetic.
// Booleans and strings are not numbers and report ok=false.
func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	default:
		return 0, false
	}
}

// evalBinary applies a comparison or arithmetic operator. Ordering comparisons
// against non-numbers return false rather than erroring, keeping guards on
// missing State fields total. Division by zero is the one hard error.
func evalBinary(op string, l, r any) (any, error) {
	switch op {
	case "==":
		return looseEqual(l, r), nil
	case "!=":
		return !looseEqual(l, r), nil
	case "<", "<=", ">", ">=":
		return compare(op, l, r), nil
	case "+", "-", "*", "/":
		return arithmetic(op, l, r)
	default:
		return nil, fmt.Errorf("unknown operator %q", op)
	}
}

// looseEqual compares two values: numbers compare by float value across int and
// float, everything else by type-aware equality. Mismatched types are unequal.
func looseEqual(l, r any) bool {
	if lf, lok := toFloat(l); lok {
		if rf, rok := toFloat(r); rok {
			return lf == rf
		}
		return false
	}
	switch lv := l.(type) {
	case string:
		rv, ok := r.(string)
		return ok && lv == rv
	case bool:
		rv, ok := r.(bool)
		return ok && lv == rv
	case nil:
		return r == nil
	default:
		return l == r
	}
}

// compare evaluates an ordering operator on two values, returning false when
// either operand is not a number.
func compare(op string, l, r any) bool {
	lf, lok := toFloat(l)
	rf, rok := toFloat(r)
	if !lok || !rok {
		return false
	}
	switch op {
	case "<":
		return lf < rf
	case "<=":
		return lf <= rf
	case ">":
		return lf > rf
	default: // ">="
		return lf >= rf
	}
}

// arithmetic evaluates +,-,*,/ on numbers; + also concatenates two strings.
func arithmetic(op string, l, r any) (any, error) {
	if op == "+" {
		if ls, ok := l.(string); ok {
			if rs, ok := r.(string); ok {
				return ls + rs, nil
			}
		}
	}
	lf, lok := toFloat(l)
	rf, rok := toFloat(r)
	if !lok || !rok {
		return nil, fmt.Errorf("cannot apply %q to %v and %v", op, l, r)
	}
	switch op {
	case "+":
		return lf + rf, nil
	case "-":
		return lf - rf, nil
	case "*":
		return lf * rf, nil
	default: // "/"
		if rf == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return lf / rf, nil
	}
}

// evalCall evaluates the tiny stdlib: len(x), has(map, key), empty(x). No other
// function names reach here — the parser rejects them at compile time.
func evalCall(name string, args []Expression, s State) (any, error) {
	vals := make([]any, len(args))
	for i, a := range args {
		v, err := a.Eval(s)
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}

	switch name {
	case "len":
		if len(vals) != 1 {
			return nil, fmt.Errorf("len() takes 1 argument, got %d", len(vals))
		}
		return float64(lengthOf(vals[0])), nil
	case "empty":
		if len(vals) != 1 {
			return nil, fmt.Errorf("empty() takes 1 argument, got %d", len(vals))
		}
		return lengthOf(vals[0]) == 0, nil
	case "has":
		if len(vals) != 2 {
			return nil, fmt.Errorf("has() takes 2 arguments, got %d", len(vals))
		}
		m, ok := asMap(vals[0])
		if !ok {
			return false, nil
		}
		key, ok := vals[1].(string)
		if !ok {
			return false, nil
		}
		_, present := m[key]
		return present, nil
	default:
		return nil, fmt.Errorf("unknown function %q", name)
	}
}

// lengthOf returns the element/character count for the kinds the stdlib spans:
// strings, slices, and maps. Everything else (including nil) is length 0.
func lengthOf(v any) int {
	switch x := v.(type) {
	case nil:
		return 0
	case string:
		return len(x)
	case []any:
		return len(x)
	case []string:
		return len(x)
	case map[string]any:
		return len(x)
	case State:
		return len(x)
	default:
		return 0
	}
}

// stringify renders a value for string interpolation. nil becomes the empty
// string and floats with no fractional part drop the trailing ".0".
func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}
