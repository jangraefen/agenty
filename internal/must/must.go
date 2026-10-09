// Package must turns errors that cannot happen into panics, so they are never
// silently discarded.
//
// The project's lint rules forbid discarding an error, even with _. Some
// calls return an error their arguments rule out, such as json.Marshal of a
// string or a plain struct; must.Value states that assumption in the code,
// and a panic shows at once if it ever turns out wrong, rather than a
// silently wrong value. It is used across the backend, by package store
// among others; it has no dependencies.
package must

// Value returns v, or panics with err if it is not nil. Use it only where err
// cannot happen, such as JSON-encoding a string.
func Value[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
