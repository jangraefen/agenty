// Package must turns errors that cannot happen into panics, so they are never
// silently discarded.
package must

// Value returns v, or panics with err if it is not nil. Use it only where err
// cannot happen, such as JSON-encoding a string.
func Value[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
