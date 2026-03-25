# ai-proxy 本地 Higress 调试手册（macOS）

这份文档只覆盖 macOS 本地开发路径，目标是从 0 到 1 跑通：

- 本地构建 `ai-proxy` wasm
- 在 kind + Higress 中加载本地 wasm
- 绑定本地 `8080` 端口
- 配置 Bedrock 本地路由
- 查看 Higress / wasm 日志
- 用 `curl` 验证普通调用、`cache_control`、`eager_input_streaming`
- 补充本地 `local/` Envoy 配置的写法

这份文档优先推荐 **Higress 本地链路**。`local/` 目录下的 Envoy 配置作为可选补充，仅用于配置参考。

## 1. 前置条件

本地需要：

- macOS
- OrbStack
- `go`
- `kind`
- `kubectl`
- `helm`

推荐用 Homebrew 安装：

```bash
brew install go kind kubectl helm
```

确认工具可用：

```bash
go version
kind version
kubectl version --client
helm version
docker version
```

## 2. 代码与目录

以下目录是本地开发的核心路径：

- 插件目录：`/Users/jkma/programfiles/codinghere/ailabgo/ai-gateway/plugins/wasm-go/extensions/ai-proxy`
- 本地 wasm 产物：`/Users/jkma/programfiles/codinghere/ailabgo/ai-gateway/plugins/wasm-go/extensions/ai-proxy/plugin.wasm`
- 本地 Envoy 配置目录：`/Users/jkma/programfiles/codinghere/ailabgo/ai-gateway/plugins/wasm-go/extensions/ai-proxy/local`
- 本地 Envoy 配置：`/Users/jkma/programfiles/codinghere/ailabgo/ai-gateway/plugins/wasm-go/extensions/ai-proxy/local/envoy.local.yaml`
- 本地 Bedrock Envoy 配置：`/Users/jkma/programfiles/codinghere/ailabgo/ai-gateway/plugins/wasm-go/extensions/ai-proxy/local/envoy-bedrock.yaml`

注意：

- `local/` 目录已经被插件目录下的 `.gitignore` 忽略，不会误提交。

## 3. 本地构建 wasm

进入插件目录：

```bash
cd /Users/jkma/programfiles/codinghere/ailabgo/ai-gateway/plugins/wasm-go/extensions/ai-proxy
```

直接在宿主机编译：

```bash
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o ./plugin.wasm .
```

说明：

- 这是当前本地最稳的构建方式。
- 相比 Docker builder，这条路径更适合本地反复调试。

### 3.1 可选：按官方方式构建 Docker / OCI 镜像

如果你要验证 `WasmPlugin.spec.url: oci://...` 这条路径，也可以按 Higress 官方文档里的方式，先编译 wasm，再打一个最小镜像。

先在插件目录生成 wasm：

```bash
cd /Users/jkma/programfiles/codinghere/ailabgo/ai-gateway/plugins/wasm-go/extensions/ai-proxy
go mod tidy
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o main.wasm ./
```

然后用一个最小 Dockerfile 打镜像：

```dockerfile
FROM scratch
COPY main.wasm plugin.wasm
```

例如保存为 `local/Dockerfile.plugin` 后执行：

```bash
docker build -t <your_registry>/ai-proxy:local -f local/Dockerfile.plugin .
docker push <your_registry>/ai-proxy:local
```

如果后面想让 Higress 从 OCI 拉取插件，可以把 `WasmPlugin.spec.url` 改成：

```yaml
url: oci://<your_registry>/ai-proxy:local
```

说明：

- 这条路径更适合验证 OCI 分发，而不是本地高频调试。
- 本文档的主路径仍然推荐直接使用本地 `plugin.wasm` + kind 节点挂载。

## 4. 启动本地 kind + Higress

如果还没有 kind 集群，可以先创建一个：

```bash
kind create cluster --name higress
```

创建命名空间：

```bash
kubectl create namespace higress-system
```

在仓库根目录安装 Higress：

```bash
cd /Users/jkma/programfiles/codinghere/ailabgo/ai-gateway
helm install higress helm/core \
  -n higress-system \
  --set controller.tag=2.1.9 \
  --set pilot.tag=2.1.9 \
  --set gateway.tag=2.1.9 \
  --set global.local=true \
  --set global.volumeWasmPlugins=true \
  --set global.onlyPushRouteCluster=false
```

确认网关 ready：

```bash
kubectl -n higress-system rollout status deployment/higress-gateway
kubectl -n higress-system get pod -l app=higress-gateway -o wide
```

## 5. 确认 kind 节点挂载 `/opt/plugins`

Higress 本地模式会把宿主挂载目录暴露给网关容器，当前使用的是：

- kind 节点容器路径：`/opt/plugins`
- 插件 wasm 实际加载路径：`file:///opt/plugins/wasm-go/extensions/ai-proxy/plugin.wasm`

查看当前网关 deployment：

```bash
kubectl -n higress-system get deployment higress-gateway -o yaml
```

重点确认：

- `volumeMounts.mountPath: /opt/plugins`
- `volumes.hostPath.path: /opt/plugins`

## 6. 把本地 wasm 拷贝进 kind 节点

先确认 kind 控制平面容器名。这里默认使用 OrbStack 提供的 `docker` CLI：

```bash
docker ps --format '{{.Names}}'
```

当前本地环境使用的是：

- `higress-control-plane`

把最新的 wasm 拷进去：

```bash
docker cp /Users/jkma/programfiles/codinghere/ailabgo/ai-gateway/plugins/wasm-go/extensions/ai-proxy/plugin.wasm \
  higress-control-plane:/opt/plugins/wasm-go/extensions/ai-proxy/plugin.wasm
```

然后重启网关：

```bash
kubectl -n higress-system rollout restart deployment/higress-gateway
kubectl -n higress-system rollout status deployment/higress-gateway
```

## 7. 配置本地 Bedrock 路由

本地成功验证过的结构是：

- `McpBridge` 指向 `bedrock-runtime.us-west-2.amazonaws.com:443`
- `Ingress` 暴露本地 host：`bedrock.local`
- `WasmPlugin` 绑定到 `bedrock-local`

### 7.1 McpBridge

```yaml
apiVersion: networking.higress.io/v1
kind: McpBridge
metadata:
  name: default
  namespace: higress-system
spec:
  registries:
    - name: bedrock-runtime
      type: dns
      domain: bedrock-runtime.us-west-2.amazonaws.com
      port: 443
```

应用：

```bash
kubectl apply -f mcpbridge.yaml
```

### 7.2 Ingress

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: bedrock-local
  namespace: higress-system
  labels:
    higress.io/resource-definer: higress
  annotations:
    higress.io/backend-protocol: HTTPS
    higress.io/destination: bedrock-runtime.dns
    higress.io/proxy-ssl-name: bedrock-runtime.us-west-2.amazonaws.com
    higress.io/proxy-ssl-server-name: "on"
spec:
  ingressClassName: higress
  rules:
    - host: bedrock.local
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              resource:
                apiGroup: networking.higress.io
                kind: McpBridge
                name: default
```

应用：

```bash
kubectl apply -f ingress-bedrock-local.yaml
```

### 7.3 WasmPlugin

```yaml
apiVersion: extensions.higress.io/v1alpha1
kind: WasmPlugin
metadata:
  name: ai-proxy-local
  namespace: higress-system
spec:
  url: file:///opt/plugins/wasm-go/extensions/ai-proxy/plugin.wasm
  priority: 100
  phase: UNSPECIFIED_PHASE
  defaultConfigDisable: true
  matchRules:
    - ingress:
        - bedrock-local
      config:
        provider:
          type: bedrock
          awsAccessKey: "<AWS_ACCESS_KEY>"
          awsSecretKey: "<AWS_SECRET_KEY>"
          awsRegion: "us-west-2"
          modelMapping:
            "*": "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
```

应用：

```bash
kubectl apply -f wasmplugin-ai-proxy-local.yaml
```

重要说明：

- `matchRules.ingress` 必须写 `bedrock-local`
- 不要写成 `higress-system/bedrock-local`

当前本地生效配置可以这样查看：

```bash
kubectl -n higress-system get mcpbridge -o yaml
kubectl -n higress-system get ingress bedrock-local -o yaml
kubectl -n higress-system get wasmplugin ai-proxy-local -o yaml
```

## 8. 绑定本地 8080 端口

把 Higress 网关端口转发到本地：

```bash
kubectl -n higress-system port-forward svc/higress-gateway 8080:80
```

成功后，本地统一入口就是：

```text
http://127.0.0.1:8080
```

## 9. 查看日志

先拿当前网关 pod：

```bash
kubectl -n higress-system get pod -l app=higress-gateway -o wide
```

看全量日志：

```bash
kubectl -n higress-system logs pod/<gateway-pod> -c higress-gateway -f
```

只看 wasm 日志：

```bash
kubectl -n higress-system logs pod/<gateway-pod> -c higress-gateway -f | grep wasm
```

只看 access log：

```bash
kubectl -n higress-system logs pod/<gateway-pod> -c higress-gateway -f | grep '"authority":"bedrock.local"'
```

### 9.1 access log、wasm log、ai_log 的区别

- `kubectl logs` 里看到的 `info cache ...`、`Envoy proxy is ready` 是网关进程日志
- `wasm log ... [ai-proxy] ...` 是插件运行日志
- JSON access log 里的 `"ai_log":"..."` 是 Envoy access log 显式打印的 filter state

如果你想做“单请求原子调试”，`ai_log` 比普通 stdout 更稳。

### 9.2 调高本地日志级别

查看当前 args：

```bash
kubectl -n higress-system get deployment higress-gateway -o jsonpath='{.spec.template.spec.containers[0].args}'
```

本地调试建议用：

- `--proxyLogLevel=info`
- `--proxyComponentLogLevel=misc:info`
- `--log_output_level=default:debug`

注意：

- `ai-proxy` 里如果使用 `log.Debugf(...)` 打日志，例如查看 Bedrock 最终 transformed request body，则必须把启动级别调到 `debug` 才能看到。
- 如果只是 access log 或普通 `info` 级别日志，不一定需要把三项都调到 `debug`。

直接 patch：

```bash
kubectl -n higress-system patch deployment higress-gateway --type='json' -p='[
  {
    "op":"replace",
    "path":"/spec/template/spec/containers/0/args",
    "value":[
      "proxy",
      "router",
      "--domain",
      "$(POD_NAMESPACE).svc.cluster.local",
      "--proxyLogLevel=info",
      "--proxyComponentLogLevel=misc:info",
      "--log_output_level=default:debug",
      "--serviceCluster=higress-gateway"
    ]
  }
]'
```

等待 rollout：

```bash
kubectl -n higress-system rollout status deployment/higress-gateway
```

## 10. curl 调用示例

### 10.1 基础 Bedrock 调用

```bash
curl http://127.0.0.1:8080/v1/messages \
  -H 'Host: bedrock.local' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "anything",
    "max_tokens": 64,
    "messages": [
      {
        "role": "user",
        "content": "你好，你是谁？"
      }
    ]
  }'
```

### 10.2 OpenAI 协议调用

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Host: bedrock.local' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "anything",
    "stream": false,
    "messages": [
      {
        "role": "user",
        "content": "查一下杭州天气，然后总结。"
      }
    ]
  }'
```

### 10.3 `cache_control` 调试示例

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Host: bedrock.local' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "anything",
    "stream": false,
    "max_tokens": 64,
    "tools": [
      {
        "type": "function",
        "cache_control": {
          "type": "ephemeral"
        },
        "function": {
          "name": "get_weather",
          "description": "Get weather by city",
          "parameters": {
            "type": "object",
            "properties": {
              "city": {
                "type": "string"
              }
            },
            "required": ["city"]
          }
        }
      }
    ],
    "messages": [
      {
        "role": "system",
        "content": [
          {
            "type": "text",
            "text": "这里放一段稳定且足够长的 system prompt",
            "cache_control": {
              "type": "ephemeral"
            }
          }
        ]
      },
      {
        "role": "user",
        "content": "查一下杭州天气，然后总结。"
      }
    ]
  }'
```

观察返回里的：

- `usage.cache_creation_input_tokens`
- `usage.cache_read_input_tokens`
- `usage.prompt_tokens_details.cached_tokens`

### 10.4 `eager_input_streaming` 调试示例

OpenAI 协议里直接写在 tool 上：

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Host: bedrock.local' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "anything",
    "stream": true,
    "tools": [
      {
        "type": "function",
        "eager_input_streaming": true,
        "function": {
          "name": "get_weather",
          "description": "Get weather by city",
          "parameters": {
            "type": "object",
            "properties": {
              "city": {
                "type": "string"
              }
            },
            "required": ["city"]
          }
        }
      }
    ],
    "messages": [
      {
        "role": "user",
        "content": "查一下杭州天气，然后总结。"
      }
    ]
  }'
```

当前 ai-proxy 的 Bedrock 行为是：

- 接收 OpenAI body 里的 `tools[*].eager_input_streaming = true`
- 自动转换成：

```json
"additionalModelRequestFields": {
  "anthropic_beta": [
    "fine-grained-tool-streaming-2025-05-14"
  ]
}
```

这点可以从 wasm log 和 `ai_log` 中确认。

如果你要看插件里新增的通用 Bedrock 原始请求日志：

- 日志内容是 `"[bedrock] transformed request body: ..."`
- 该日志使用 `log.Debugf(...)`
- 所以必须先把 Higress 网关启动级别调到 `debug`

## 11. Optional：本地 `local/` Envoy 配置

文件路径：

- [local/envoy.local.yaml](/Users/jkma/programfiles/codinghere/ailabgo/ai-gateway/plugins/wasm-go/extensions/ai-proxy/local/envoy.local.yaml)
- [local/envoy-bedrock.yaml](/Users/jkma/programfiles/codinghere/ailabgo/ai-gateway/plugins/wasm-go/extensions/ai-proxy/local/envoy-bedrock.yaml)

这些文件适合做结构参考，当前内容已经是本地调试版本，主要字段包括：

- `awsAccessKey`
- `awsSecretKey`
- `awsRegion`
- `modelMapping`
- upstream cluster 指向 `bedrock-runtime.us-west-2.amazonaws.com:443`

注意：

- 当前 `local/` 目录下的配置是本地私有配置
- `local/` 目录已被忽略，不会进 git
- `ai-proxy` 当前更推荐直接跑 **Higress Gateway**，不要默认依赖 stock Envoy

推荐把它当作：

- provider 配置模板
- Bedrock upstream/SNI 示例
- 本地快速改配置的参考文件

## 12. 常见问题

### 12.1 `route_not_found`

通常是 host 不对。

比如：

- `Host: anthropic.local` 但本地没配这条 ingress

当前本文档的主路径只保证：

- `Host: bedrock.local`

### 12.2 请求打通了，但没有缓存命中字段

优先排查：

- 请求前缀太短
- `cache_control` 打在太靠后的块上
- 模型或 profile 没返回 cache usage

不是只要带了 `cache_control` 就一定会看到缓存 token 字段。

### 12.3 access log 里 `ai_log` 是 `"-"`

表示当前请求没有往 `wasm.ai_log` 写内容，不代表插件没生效。

### 12.4 看到 access log，但看不到 wasm 业务日志

优先检查：

- 网关日志级别是否调高
- 当前请求是否真的命中 ai-proxy
- 是否只是在看 access log，而不是完整容器日志

### 12.5 `No rule to make target help`

这个仓库的 `make` 目标不是统一提供 `help`，直接用明确的命令即可，不需要先跑 `make help`。

## 13. 推荐的最短调试路径

如果你只想最快复现本地 Bedrock 调试，按下面顺序走：

1. 编译 wasm：

```bash
cd /Users/jkma/programfiles/codinghere/ailabgo/ai-gateway/plugins/wasm-go/extensions/ai-proxy
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o ./plugin.wasm .
```

2. 拷到 kind 节点并重启 Higress：

```bash
docker cp /Users/jkma/programfiles/codinghere/ailabgo/ai-gateway/plugins/wasm-go/extensions/ai-proxy/plugin.wasm \
  higress-control-plane:/opt/plugins/wasm-go/extensions/ai-proxy/plugin.wasm

kubectl -n higress-system rollout restart deployment/higress-gateway
kubectl -n higress-system rollout status deployment/higress-gateway
```

3. 本地绑定 `8080`：

```bash
kubectl -n higress-system port-forward svc/higress-gateway 8080:80
```

4. 看日志：

```bash
kubectl -n higress-system get pod -l app=higress-gateway -o wide
kubectl -n higress-system logs pod/<gateway-pod> -c higress-gateway -f
```

5. 发请求：

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Host: bedrock.local' \
  -H 'Content-Type: application/json' \
  -d '{"model":"anything","messages":[{"role":"user","content":"你好，你是谁？"}]}'
```
