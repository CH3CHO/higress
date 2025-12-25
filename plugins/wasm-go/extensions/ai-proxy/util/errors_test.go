package util

import (
	"testing"
)

func TestIsBadRequestErr(t *testing.T) {
	err := BadRequest("tool_choice='none' is unsupported")
	if !IsBadRequestErr(err) {
		t.Errorf("IsBadRequestErr should return true for wrapped ErrBadRequest")
	}
}
