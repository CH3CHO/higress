package util

func Ptr[T any](v T) *T {
	return &v
}

var (
	boolTrue  = true
	boolFalse = false
)

func BoolPtr(v bool) *bool {
	if v {
		return &boolTrue
	}
	return &boolFalse
}
