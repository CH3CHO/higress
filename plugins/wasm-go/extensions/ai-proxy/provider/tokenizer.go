package provider

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/higress-group/wasm-go/pkg/wrapper"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type TokenizerConfig struct {
	// @Title zh-CN Tokenizer服务名称
	// @Description zh-CN Tokenizer服务在服务列表中的名称
	serviceName string `required:"true" yaml:"serviceName" json:"serviceName"`
	// @Title zh-CN Tokenizer服务端口
	// @Description zh-CN Tokenizer服务在服务列表中的端口
	servicePort int64 `required:"true" yaml:"servicePort" json:"servicePort"`
	// @Title zh-CN Tokenizer服务主机头
	// @Description zh-CN Tokenizer服务的主机头，用于HTTP请求头中的Host字段
	serviceHost string `required:"false" yaml:"serviceHost" json:"serviceHost"`
	// @Title zh-CN Tokenizer服务路径
	// @Description zh-CN Tokenizer服务的请求路径。 默认为/token/decode
	servicePath string `required:"false" yaml:"servicePath" json:"servicePath"`
	// @Title zh-CN Tokenizer服务请求超时时间
	// @Description zh-CN Tokenizer服务请求超时时间，单位为毫秒。默认值为10000毫秒
	timeout uint32 `required:"false" yaml:"timeout" json:"timeout"`
	// @Title zh-CN 模型名称到分词编码的映射表
	// @Description zh-CN 用于指定不同模型使用不同的分词编码。支持使用 * 通配符匹配模型名称后缀或全局匹配。默认值为空，表示使用默认的编码方式
	encodingMapping map[string]string
}

func (tc *TokenizerConfig) FromJson(json gjson.Result) {
	tc.serviceName = json.Get("serviceName").String()
	tc.servicePort = json.Get("servicePort").Int()
	tc.serviceHost = json.Get("serviceHost").String()
	tc.servicePath = json.Get("servicePath").String()
	if tc.servicePath == "" {
		tc.servicePath = "/token/decode"
	}
	tc.timeout = uint32(json.Get("timeout").Uint())
	if tc.timeout == 0 {
		tc.timeout = 10000
	}
	tc.encodingMapping = make(map[string]string)
	if encodingMappingJson := json.Get("encodingMapping"); encodingMappingJson.IsObject() {
		encodingMappingJson.ForEach(func(key, value gjson.Result) bool {
			tc.encodingMapping[key.String()] = value.String()
			return true
		})
	}
}

func (tc *TokenizerConfig) Validate() error {
	if tc.serviceName == "" {
		return errors.New("missing serviceName in tokenizer config")
	}
	if tc.servicePort <= 0 || tc.servicePort > 65535 {
		return errors.New("invalid servicePort in tokenizer config")
	}
	return nil
}

type Tokenizer interface {
	Decode(llmModel string, tokenizedInput gjson.Result, callback func(gjson.Result, error)) error
}

type TokenizerAware interface {
	SetTokenizer(tokenizer Tokenizer)
}

func CreateTokenizer(tc TokenizerConfig) Tokenizer {
	return &tokenizer{
		config: tc,
		client: wrapper.NewClusterClient(wrapper.FQDNCluster{
			FQDN: tc.serviceName,
			Port: tc.servicePort,
			Host: tc.serviceHost,
		}),
	}
}

type tokenizer struct {
	config TokenizerConfig
	client wrapper.HttpClient
}

func (t *tokenizer) Decode(llmModel string, tokenizedInput gjson.Result, callback func(gjson.Result, error)) (err error) {
	var request []byte
	if encoding := performPatternMapping(llmModel, t.config.encodingMapping); encoding != "" {
		// set encoding only if there is a mapping for the given model
		if request, err = sjson.SetBytes(request, "encoding", t.config.encodingMapping[llmModel]); err != nil {
			return err
		}
	}
	if request, err = sjson.SetRawBytes(request, "input", []byte(tokenizedInput.Raw)); err != nil {
		return err
	}
	err = t.client.Post(t.config.servicePath, [][2]string{{"content-type", "application/json"}}, request, func(statusCode int, responseHeaders http.Header, responseBody []byte) {
		if statusCode != 200 {
			callback(gjson.Result{}, fmt.Errorf("got error in tokenizer service call. code=%d body=%s", statusCode, string(responseBody)))
			return
		}
		callback(gjson.GetBytes(responseBody, "output"), nil)
	}, t.config.timeout)
	return
}
