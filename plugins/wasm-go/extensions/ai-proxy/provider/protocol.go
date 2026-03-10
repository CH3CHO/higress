package provider

import (
	"strings"

	"github.com/higress-group/wasm-go/pkg/wrapper"
)

const (
	originalRequestPathKeyword = "/raw/"
)

func SaveProtocolToContext(ctx wrapper.HttpContext, providerConfig *ProviderConfig) {
	if providerConfig.IsOriginal() {
		ctx.SetContext("protocol", protocolOriginal)
		return
	}

	if strings.Contains(ctx.Path(), originalRequestPathKeyword) {
		ctx.SetContext("protocol", protocolOriginal)
		return
	}

	ctx.SetContext("protocol", protocolOpenAI)
}

func GetProtocolFromContext(ctx wrapper.HttpContext) string {
	return ctx.GetStringContext("protocol", protocolOpenAI)
}

func IsOriginalProtocol(ctx wrapper.HttpContext) bool {
	protocol := GetProtocolFromContext(ctx)
	return protocol == protocolOriginal
}
