package json

type JsonScope int

const (
	// An array with no elements requires no separator before the next element.
	JsonScopeEmptyArray JsonScope = iota
	// An array with at least one value requires a separator before the next element.
	JsonScopeNonEmptyArray
	// An object with no name/value pairs requires no separator before the next element.
	JsonScopeEmptyObject
	// An object whose most recent element is a key. The next element must be a value.
	JsonScopeDanglingName
	// An object with at least one name/value pair requires a separator before the next element.
	JsonScopeNonEmptyObject
	// No top-level value has been started yet.
	JsonScopeEmptyDocument
	// A top-level value has already been started.
	JsonScopeNonEmptyDocument
)
