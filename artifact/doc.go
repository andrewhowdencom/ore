// Package artifact defines the extensible Artifact interface and common concrete
// types used throughout ore.
//
// The Artifact interface exposes a public Kind() method to allow custom artifact
// types to be defined in other packages. Private marker methods would prevent
// cross-package extensibility because Go does not allow implementing unexported
// methods across package boundaries.
package artifact
