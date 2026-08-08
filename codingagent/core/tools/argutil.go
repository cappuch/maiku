package tools

// Small helpers for extracting typed values out of the map[string]any tool
// arguments produced by JSON-Schema-validated tool calls (numbers decode as
// float64, arrays as []any, objects as map[string]any).

func argString(args map[string]any, key string) (string, bool) {
	v, ok := args[key]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func argStringOr(args map[string]any, key string, def string) string {
	if s, ok := argString(args, key); ok {
		return s
	}
	return def
}

func argNumber(args map[string]any, key string) (float64, bool) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func argIntPtr(args map[string]any, key string) *int {
	n, ok := argNumber(args, key)
	if !ok {
		return nil
	}
	i := int(n)
	return &i
}

func argBool(args map[string]any, key string) (bool, bool) {
	v, ok := args[key]
	if !ok || v == nil {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}
