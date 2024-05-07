package config

import (
	"fmt"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/provider"
	"github.com/tidwall/gjson"
)

// @Name ai-proxy
// @Category custom
// @Phase UNSPECIFIED_PHASE
// @Priority 0
// @Title zh-CN AI代理
// @Description zh-CN 通过AI助手提供智能对话服务
// @IconUrl https://img.alicdn.com/imgextra/i1/O1CN018iKKih1iVx287RltL_!!6000000004419-2-tps-42-42.png
// @Version 0.1.0
//
// @Contact.name CH3CHO
// @Contact.url https://github.com/CH3CHO
// @Contact.email ch3cho@qq.com
//
// @Example
// {"activeProvider": {"type": "moonshot", "domain": "api.moonshot.cn", "apiToken": "sk-1234567890", "model": "moonshot-v1-128k", "fileId": "abcd1234", "timeout": 1200000 } }
// @End
type PluginConfig struct {
	// @Title zh-CN AI服务提供商配置
	// @Description zh-CN AI服务提供商配置，包含API接口、模型和知识库文件等信息
	providers []provider.ProviderConfig `required:"true" yaml:"providers"`
	// @Title zh-CN 模型对话上下文
	// @Description zh-CN 配置一个外部获取对话上下文的文件来源，用于在AI请求中补充对话上下文
	contexts []provider.ContextConfig `required:"false" yaml:"contexts"`
	// @Title zh-CN 当前启用的AI服务提供商
	// @Description zh-CN 当前启用的AI服务提供商标识
	activeProviderId string `required:"true" yaml:"activeProvider"`
	// @Title zh-CN 当前启用的对话上下文
	// @Description zh-CN 当前启用的AI服务提供商标识
	activeContextId string `required:"true" yaml:"activeContext"`

	activeProvider provider.Provider `yaml:"-"`
}

func (c *PluginConfig) FromJson(json gjson.Result) {
	providersJson := json.Get("providers")
	if providersJson.IsArray() {
		for _, providerJson := range providersJson.Array() {
			providerConfig := provider.ProviderConfig{}
			providerConfig.FromJson(providerJson)
			c.providers = append(c.providers, providerConfig)
		}
	}
	contextsJson := json.Get("contexts")
	if contextsJson.IsArray() {
		for _, contextJson := range contextsJson.Array() {
			contextConfig := provider.ContextConfig{}
			contextConfig.FromJson(contextJson)
			c.contexts = append(c.contexts, contextConfig)
		}
	}
	c.activeProviderId = json.Get("activeProvider").String()
	c.activeContextId = json.Get("activeContext").String()
}

func (c *PluginConfig) Validate() error {
	if c.providers != nil && len(c.providers) != 0 {
		for _, p := range c.providers {
			if err := p.Validate(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *PluginConfig) Complete() error {
	if c.activeProviderId == "" {
		c.activeProvider = nil
		return nil
	}
	for _, p := range c.providers {
		if p.GetId() != c.activeProviderId {
			continue
		}
		var err error
		if c.activeProvider, err = provider.CreateProvider(p); err != nil {
			return err
		}
		if c.activeContextId != "" {
			if c.contexts == nil {
				return fmt.Errorf("missing contexts")
			}
			contextFound := false
			for _, context := range c.contexts {
				if context.GetId() != c.activeContextId {
					continue
				}
				if err := c.activeProvider.InitializeContext(context); err != nil {
					return err
				}
				contextFound = true
				break
			}
			if !contextFound {
				return fmt.Errorf("unknown active context: %s", c.activeContextId)
			}
		}
		return nil
	}
	return fmt.Errorf("unknown active provider: %s", c.activeProviderId)
}

func (c *PluginConfig) GetProvider() provider.Provider {
	return c.activeProvider
}
