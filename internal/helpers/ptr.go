package helpers

// Ptr is a helper that returns a pointer to the given value.
// Useful for creating pointers to literals or values in a single expression.
func Ptr[T any](v T) *T {
	return &v
}
