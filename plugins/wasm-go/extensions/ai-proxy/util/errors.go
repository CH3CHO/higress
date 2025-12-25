package util

import (
	"errors"
	"fmt"
)

var (
	ErrBadRequest = errors.New("bad request")
)

func BadRequest(message string) error {
	return fmt.Errorf("%w: %s", ErrBadRequest, message)
}

func IsBadRequestErr(err error) bool {
	return errors.Is(err, ErrBadRequest)
}
