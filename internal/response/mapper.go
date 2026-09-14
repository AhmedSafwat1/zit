package response

// Map converts a slice with fn. Never returns nil, so JSON encodes [] not null.
func Map[T, R any](in []T, fn func(T) R) []R {
	out := make([]R, 0, len(in))
	for _, v := range in {
		out = append(out, fn(v))
	}
	return out
}

// MapRef is Map for mappers that take a pointer to the element, avoiding a
// copy of large rows (GORM models).
func MapRef[T, R any](in []T, fn func(*T) R) []R {
	out := make([]R, 0, len(in))
	for i := range in {
		out = append(out, fn(&in[i]))
	}
	return out
}

// MapErr is Map for mappers that can fail; it stops at the first error.
func MapErr[T, R any](in []T, fn func(T) (R, error)) ([]R, error) {
	out := make([]R, 0, len(in))
	for _, v := range in {
		r, err := fn(v)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
