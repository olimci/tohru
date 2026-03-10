package cloneutils

func AnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = Any(v)
	}
	return out
}

func Any(v any) any {
	switch value := v.(type) {
	case map[string]any:
		return AnyMap(value)
	case []any:
		out := make([]any, len(value))
		for i := range value {
			out[i] = Any(value[i])
		}
		return out
	default:
		return value
	}
}
