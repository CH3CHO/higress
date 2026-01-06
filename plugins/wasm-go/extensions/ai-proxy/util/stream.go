package util

import (
	"github.com/higress-group/wasm-go/pkg/wrapper"
)

const (
	ctxKeyFakeStream = "fakeStream"
)

func IsFakeStream(ctx wrapper.HttpContext) bool {
	fakeStream, _ := ctx.GetContext(ctxKeyFakeStream).(bool)
	return fakeStream
}

func SetFakeStream(ctx wrapper.HttpContext) {
	ctx.SetContext(ctxKeyFakeStream, true)
}
