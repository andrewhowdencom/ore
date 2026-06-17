package catalogmodels

// ptr is a small helper for taking the address of a literal —
// pointer-typed Spec fields need an addressable value. The
// generated per-family files reference this helper rather than
// redeclaring it, so the whole catalog compiles to a single
// definition.
func ptr[T any](v T) *T { return &v }
