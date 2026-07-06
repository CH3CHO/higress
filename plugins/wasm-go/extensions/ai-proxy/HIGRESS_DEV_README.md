# ai-proxy 本地 Higress 调试手册（macOS）

这份文档只覆盖 macOS 本地开发路径，目标是从 0 到 1 跑通：

- 本地构建 `ai-proxy` wasm
- 在 kind + Higress 中加载本地 wasm
- 绑定本地 `8080` 端口
- 配置 Bedrock / OpenAI 本地路由
- 查看 Higress / wasm 日志
- 用 `curl` 验证普通调用、`cache_control`、fine-grained tool streaming
- 补充 standalone Envoy 的本地 `local/` 配置写法

这份文档优先推荐 **Higress 本地链路**。当前推荐把 Higress 本地资源直接维护在 `local/` 目录下：

- `local/mcpbridge-local.yaml`
- `local/ingress-bedrock-local.yaml`
- `local/ingress-openai-local.yaml`
- `local/wasmplugin-ai-proxy-local.yaml`

`local/` 目录下的 Envoy 配置仅作为 standalone Envoy 的可选补充，不参与 Higress 启动。

## 快速开始

先确认你操作的是哪套集群。本文档默认使用 `kind create cluster --name higress` 创建出来的 `kind-higress`，不是 OrbStack 内建 Kubernetes：

```bash
kubectl config current-context
kubectl get nodes -o wide
```

预期至少满足两点：

- 当前 context 是 `kind-higress`
- 节点名里能看到 `higress-control-plane`

如果你看到的是 `orbstack` 之类的 context，说明你当前连的是 OrbStack 内建集群。此时 `docker cp ... higress-control-plane:/opt/plugins/...` 改的是 kind 节点容器，但 `kubectl apply` 可能落到另一套集群，现象通常是“wasm 明明拷进去了，但 Higress 还是不生效”。

如果你只想最快复现本地 Bedrock 调试，按下面顺序走：

如果本机还没有可用的 `kind + Higress`，先跳到第 `4` 节完成安装，再回到这里继续。

先设置通用变量：

```bash
export AI_GATEWAY_REPO="<ai-gateway-repo>"
export WASM_GO_DIR="$AI_GATEWAY_REPO/plugins/wasm-go"
export AI_PROXY_DIR="$WASM_GO_DIR/extensions/ai-proxy"
export AI_PROXY_LOCAL_DIR="$AI_PROXY_DIR/local"
export WASM_OUT="$AI_PROXY_DIR/plugin.wasm"
export KIND_NODE="<kind-node-container>"
export WASM_BUILDER_IMAGE="higress-registry.cn-hangzhou.cr.aliyuncs.com/plugins/wasm-go-builder:go1.24.0-oras1.0.0"
```

这里优先走仓库自带的 wasm builder 镜像，而不是直接用宿主机 `go build`。这样可以避免本机 Go 版本、环境变量或依赖差异影响 `plugin.wasm` 产物。

1. 编译 wasm：

```bash
cd "$WASM_GO_DIR"
DOCKER_BUILDKIT=1 docker build \
  --build-arg BUILDER="$WASM_BUILDER_IMAGE" \
  --build-arg PLUGIN_NAME=ai-proxy \
  -t ai-proxy:local \
  --output "type=local,dest=$AI_PROXY_DIR" .
```

2. 拷到 kind 节点：

```bash
docker cp "$WASM_OUT" \
  "$KIND_NODE:/opt/plugins/wasm-go/extensions/ai-proxy/plugin.wasm"
```

3. 应用本地 Higress 资源：

```bash
kubectl apply -f "$AI_PROXY_LOCAL_DIR/mcpbridge-local.yaml"
kubectl apply -f "$AI_PROXY_LOCAL_DIR/ingress-bedrock-local.yaml"
kubectl apply -f "$AI_PROXY_LOCAL_DIR/ingress-openai-local.yaml"
kubectl apply -f "$AI_PROXY_LOCAL_DIR/wasmplugin-ai-proxy-local.yaml"
```

推荐先把这些文件按本地环境改成类似下面的结构：

`local/mcpbridge-local.yaml`

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
    - name: aigc-swedencentral.openai.azure.com
      type: dns
      domain: aigc-swedencentral.openai.azure.com
      port: 443
```

`local/ingress-bedrock-local.yaml`

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

`local/wasmplugin-ai-proxy-local.yaml`

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
            claude-sonnet-4-5: global.anthropic.claude-sonnet-4-5-20250929-v1:0
            claude-sonnet-4-6: global.anthropic.claude-sonnet-4-6
    - ingress:
        - openai-local
      config:
        provider:
          type: openai
          apiTokens:
            - "<OPENAI_API_TOKEN>"
          openaiCustomUrl: "https://aigc-swedencentral.openai.azure.com/openai/v1"
```

注意：

- 文档里不放真实 token / secret，只保留占位符。
- 真正生效的是集群里的 `WasmPlugin`，改完文件后要重新 `kubectl apply`。
- `bedrock.local` 和 `openai.local` 都依赖 `McpBridge default`；如果没先 apply `local/mcpbridge-local.yaml`，路由链路不会闭合。
- 这组 `modelMapping` 已在本地 Higress 上验证过 4 种组合：
  `claude-sonnet-4-5` 的 `/v1/messages`、`/v1/chat/completions`，
  以及 `claude-sonnet-4-6` 的 `/v1/messages`、`/v1/chat/completions`。

4. 重启 Higress：

```bash
kubectl -n higress-system rollout restart deployment/higress-gateway
kubectl -n higress-system rollout status deployment/higress-gateway --timeout=180s
```

5. 确认本地 Bedrock 路由资源已经存在：

```bash
kubectl -n higress-system get mcpbridge
kubectl -n higress-system get ingress bedrock-local openai-local
kubectl -n higress-system get wasmplugin ai-proxy-local
```

6. 本地绑定 `8080`：

```bash
kubectl -n higress-system port-forward deployment/higress-gateway 8080:80
```

如果 `8080` 已被旧的 `kubectl port-forward` 占用，可以先结束旧进程，或者改用 `18080:80`。

7. 看日志：

```bash
kubectl -n higress-system get pod -l app=higress-gateway -o wide
kubectl -n higress-system logs pod/<gateway-pod> -c higress-gateway -f
```

8. 发请求：

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Host: bedrock.local' \
  -H 'Content-Type: application/json' \
  -d '{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"你好，你是谁？"}]}'
```

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

- 插件目录：`<ai-gateway-repo>/plugins/wasm-go/extensions/ai-proxy`
- 本地 wasm 产物：`<ai-gateway-repo>/plugins/wasm-go/extensions/ai-proxy/plugin.wasm`
- Higress 本地 Ingress 配置：`<ai-gateway-repo>/plugins/wasm-go/extensions/ai-proxy/local/ingress-bedrock-local.yaml`
- Higress 本地 OpenAI Ingress 配置：`<ai-gateway-repo>/plugins/wasm-go/extensions/ai-proxy/local/ingress-openai-local.yaml`
- Higress 本地 McpBridge 配置：`<ai-gateway-repo>/plugins/wasm-go/extensions/ai-proxy/local/mcpbridge-local.yaml`
- Higress 本地 WasmPlugin 配置：`<ai-gateway-repo>/plugins/wasm-go/extensions/ai-proxy/local/wasmplugin-ai-proxy-local.yaml`
- standalone Envoy 配置目录：`<ai-gateway-repo>/plugins/wasm-go/extensions/ai-proxy/local`
- standalone Envoy 配置：`<ai-gateway-repo>/plugins/wasm-go/extensions/ai-proxy/local/envoy.local.yaml`
- standalone Bedrock Envoy 配置：`<ai-gateway-repo>/plugins/wasm-go/extensions/ai-proxy/local/envoy-bedrock.yaml`

注意：

- `local/` 目录已经被插件目录下的 `.gitignore` 忽略，不会误提交。

## 3. 优先：用 builder 镜像构建 wasm

进入 wasm-go 根目录：

```bash
cd <ai-gateway-repo>/plugins/wasm-go
```

推荐直接复用仓库里的 [`plugins/wasm-go/Dockerfile`](../../Dockerfile) 和 builder 镜像来产出 `ai-proxy/plugin.wasm`：

```bash
DOCKER_BUILDKIT=1 docker build \
  --build-arg BUILDER=higress-registry.cn-hangzhou.cr.aliyuncs.com/plugins/wasm-go-builder:go1.24.0-oras1.0.0 \
  --build-arg PLUGIN_NAME=ai-proxy \
  -t ai-proxy:local \
  --output "type=local,dest=./extensions/ai-proxy" .
```

说明：

- 这是当前更推荐的构建方式，和仓库里其他 wasm-go 插件的构建链路一致。
- 产物会直接落到 `extensions/ai-proxy/plugin.wasm`，后面可以直接 `docker cp` 到 kind 节点。
- 这样不用依赖宿主机 Go 版本，也不会因为本地默认 `go` 高于 `go.mod` 里的 toolchain 而打出不同产物。

### 3.1 兜底：直接在宿主机构建 wasm

如果只是临时排查，或者本机没有可用 Docker，也可以直接在插件目录用本地 Go 构建：

```bash
cd <ai-gateway-repo>/plugins/wasm-go/extensions/ai-proxy
GOTOOLCHAIN=go1.24.4 GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o ./plugin.wasm .
```

说明：

- 这里显式固定 `GOTOOLCHAIN=go1.24.4`，与 [`go.mod`](./go.mod) 里的 `toolchain go1.24.4` 保持一致。
- 这条路径保留给临时兜底，不作为本文档主推荐方案。

### 3.2 可选：按官方方式构建 Docker / OCI 镜像

如果你要验证 `WasmPlugin.spec.url: oci://...` 这条路径，也可以按 Higress 官方文档里的方式，先编译 wasm，再打一个最小镜像。

先生成 wasm。这里同样建议优先走 builder 镜像：

```bash
cd <ai-gateway-repo>/plugins/wasm-go
DOCKER_BUILDKIT=1 docker build \
  --build-arg BUILDER=higress-registry.cn-hangzhou.cr.aliyuncs.com/plugins/wasm-go-builder:go1.24.0-oras1.0.0 \
  --build-arg PLUGIN_NAME=ai-proxy \
  -t ai-proxy:local \
  --output "type=local,dest=./extensions/ai-proxy" .
cp ./extensions/ai-proxy/plugin.wasm ./extensions/ai-proxy/main.wasm
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
- 如果只是本地 Higress 调试，本文档主路径仍然推荐 builder 镜像产出本地 `plugin.wasm` 后再挂到 kind 节点。

## 4. 启动本地 kind + Higress

如果还没有 kind 集群，可以先创建一个：

```bash
kind create cluster --name higress
```

创建命名空间：

```bash
kubectl create namespace higress-system
```

在仓库根目录安装或重置本地 Higress：

```bash
cd <ai-gateway-repo>
helm upgrade --install higress helm/core \
  -n higress-system \
  --create-namespace \
  --reset-values \
  --set controller.tag=2.2.0 \
  --set pilot.tag=2.2.0 \
  --set gateway.tag=2.2.0 \
  --set global.local=true \
  --set gateway.service.type=None \
  --set global.volumeWasmPlugins=true \
  --set global.onlyPushRouteCluster=false
```

说明：

- 本地模式下网关已经使用 `hostPort 80/443` 暴露入口。
- 如果同时保留 `LoadBalancer` 类型的 `svc/higress-gateway`，k3s 的 `svclb` Pod 会占用同样的端口，导致 `higress-gateway` 调度失败。
- 因此本文档明确关闭 `gateway.service`，后续统一使用 `deployment` 级别的 `port-forward`。

确认网关 ready：

```bash
kubectl -n higress-system rollout status deployment/higress-gateway --timeout=180s
kubectl -n higress-system get pod -l app=higress-gateway -o wide
```

### 4.1 OrbStack 和 `kind-higress` 的区别

这两个名字很容易混，但在本文档里职责不同：

- `orbstack` context：指 OrbStack 自带的 Kubernetes 集群。它可以拿来跑普通 k8s 资源，但不是本文档默认的 Higress 调试环境。
- `kind-higress` context：指 `kind create cluster --name higress` 创建出来的 kind 集群。本文档默认所有 `kubectl apply`、`rollout restart`、`port-forward` 都针对它。
- `higress-control-plane`：是 `kind-higress` 里的节点容器名。`/opt/plugins` 实际就在这个 Docker 容器里。
- OrbStack 在这里主要提供 `docker` 和 kind 的运行环境，不等于“你当前 `kubectl` 正连着的就是 OrbStack 内建集群”。

最关键的一条：

- `docker cp ... higress-control-plane:/opt/plugins/...` 只会改 `kind-higress`
- 如果这时你的 `kubectl` context 还是 `orbstack`，那就是在两套集群之间来回操作，文件和资源不会汇合到同一条链路上

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

### 5.1 为什么这里要改 kind 节点，而不是直接 `kubectl exec`

这里的 `/opt/plugins` 不是 `higress-gateway` 容器镜像里自带的普通目录，而是一个 `hostPath` 挂载。

这意味着：

- `WasmPlugin.spec.url` 里看到的 `file:///opt/plugins/...`，最终读取的是 **Kubernetes 节点文件系统** 上的 `/opt/plugins`
- 在 `kind` 场景里，Kubernetes 节点本身就是一个 Docker 容器
- 所以准备本地 wasm 文件时，目标实际上是 **kind 节点容器里的 `/opt/plugins`**

因此这里要区分两层：

- 用 `kubectl` 管理集群资源，例如 `Deployment`、`Ingress`、`WasmPlugin`
- 用 `docker exec` / `docker cp` 操作 kind 节点容器里的 `/opt/plugins`

不要把这两件事混在一起。典型原因有两个：

- `kubectl exec` 进入的是 Pod 容器，不是节点根文件系统
- 如果 `hostPath` 对应目录还没准备好，`higress-gateway` Pod 可能会因为挂载失败起不来，这时也无法依赖 `kubectl exec` 进去补目录

最短判断方式：

- 如果你在处理 `Ingress`、`WasmPlugin`、`rollout restart`，用 `kubectl`
- 如果你在处理 `/opt/plugins` 目录和 `plugin.wasm` 文件，改 kind 节点容器

另外，执行这些步骤前先确认当前 `kubectl` context 就是目标 kind 集群，例如：

```bash
kubectl config current-context
kubectl get nodes -o wide
```

如果当前 context 指向的不是文档里的 kind 集群，那么 `/opt/plugins` 对应的就不是你以为的那台节点，后面的 `docker cp` 和 `kubectl apply` 会落在两套不同环境上。

## 6. 把本地 wasm 拷贝进 kind 节点

先确认 kind 控制平面容器名。这里默认使用 OrbStack 提供的 `docker` CLI：

```bash
docker ps --format '{{.Names}}'
```

当前本地环境使用的是 kind 控制面节点容器，可以先确认名字：

- `docker ps --format '{{.Names}}'`
- 常见示例：`higress-control-plane`

把最新的 wasm 拷进去：

```bash
docker cp <ai-gateway-repo>/plugins/wasm-go/extensions/ai-proxy/plugin.wasm \
  <kind-node-container>:/opt/plugins/wasm-go/extensions/ai-proxy/plugin.wasm
```

然后重启网关：

```bash
kubectl -n higress-system rollout restart deployment/higress-gateway
kubectl -n higress-system rollout status deployment/higress-gateway --timeout=180s
```

## 7. 配置本地 Bedrock / OpenAI 路由

当前本地样例支持两条入口：

- `McpBridge` 指向 `bedrock-runtime.us-west-2.amazonaws.com:443`
- `McpBridge` 同时包含 `aigc-swedencentral.openai.azure.com:443`
- `Ingress` 暴露本地 host：`bedrock.local`
- `Ingress` 暴露本地 host：`openai.local`
- `WasmPlugin` 同时绑定到 `bedrock-local` 和 `openai-local`

最小必要性说明：

- 如果你要走这份文档推荐的 Higress 本地链路，并且请求最终要从 Higress 转发到真实 Bedrock 或 OpenAI，那么这三项都需要。
- `McpBridge` 负责给 Higress 提供外部 Bedrock / OpenAI 域名对应的上游目标。
- `Ingress` 负责把本地 `Host: bedrock.local` 或 `Host: openai.local` 路由到这个上游，同时作为 `WasmPlugin.matchRules.ingress` 的绑定对象。
- `WasmPlugin` 负责加载本地 wasm，并按命中的 Ingress 把 OpenAI 风格请求转换成对应 provider 请求。
- 如果你只是做本地 Envoy 调试、单测，或者只验证 wasm 编译产物本身，那就不一定需要这三项。

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
    - name: aigc-swedencentral.openai.azure.com
      type: dns
      domain: aigc-swedencentral.openai.azure.com
      port: 443
```

应用：

```bash
kubectl apply -f local/mcpbridge-local.yaml
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
kubectl apply -f local/ingress-bedrock-local.yaml
```

如果你还要本地联调 OpenAI 兼容入口，再加一条：

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: openai-local
  namespace: higress-system
  labels:
    higress.io/resource-definer: higress
  annotations:
    higress.io/backend-protocol: HTTPS
    higress.io/destination: aigc-swedencentral.openai.azure.com.dns
    higress.io/proxy-ssl-name: aigc-swedencentral.openai.azure.com
    higress.io/proxy-ssl-server-name: "on"
spec:
  ingressClassName: higress
  rules:
    - host: openai.local
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
kubectl apply -f local/ingress-openai-local.yaml
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
            claude-sonnet-4-5: global.anthropic.claude-sonnet-4-5-20250929-v1:0
            claude-sonnet-4-6: global.anthropic.claude-sonnet-4-6
    - ingress:
        - openai-local
      config:
        provider:
          type: openai
          apiTokens:
            - "<OPENAI_API_TOKEN>"
          openaiCustomUrl: "https://aigc-swedencentral.openai.azure.com/openai/v1"
```

应用：

```bash
kubectl apply -f local/wasmplugin-ai-proxy-local.yaml
```

### 7.4 可选：叠加 ai-statistics

如果你希望在本地 Higress 链路里同时观察 `ai_log`、token 指标、流式首包耗时等信息，可以再挂一个 `ai-statistics` 本地插件：

```yaml
apiVersion: extensions.higress.io/v1alpha1
kind: WasmPlugin
metadata:
  name: ai-statistics-local
  namespace: higress-system
spec:
  url: file:///opt/plugins/wasm-go/extensions/ai-statistics/plugin.wasm
  priority: 200
  phase: UNSPECIFIED_PHASE
  defaultConfigDisable: false
  defaultConfig:
    enable: true
  matchRules:
    - ingress:
        - bedrock-local
        - openai-local
```

应用：

```bash
kubectl apply -f local/wasmplugin-ai-statistics-local.yaml
```

说明：

- `ai-proxy-local` 当前优先级是 `100`
- `ai-statistics-local` 当前优先级是 `200`
- 当前样例里 `ai-statistics-local` 同时绑定到 `bedrock-local` 和 `openai-local`
- `ai-statistics` 默认按请求路径后缀工作；现在本地也支持 `/messages` 和 `/v1/messages`

重要说明：

- `matchRules.ingress` 必须写 Ingress 资源名，例如 `bedrock-local` 或 `openai-local`
- 不要写成 `higress-system/bedrock-local`

`ai-statistics` 本地联调时，建议额外注意这几件事：

- `ai-statistics-local` 要显式覆盖你实际压测的入口。调 `bedrock.local` 至少要匹配 `bedrock-local`；调 `openai.local` 的 `/v1/responses` 时还要匹配 `openai-local`。少挂一个 ingress，对应请求就不会写 `ai_log`。
- 改 `plugins/wasm-go/extensions/ai-statistics/*.go` 之后，要重新构建 `plugins/wasm-go/extensions/ai-statistics/plugin.wasm`，再把它拷到 `higress-control-plane:/opt/plugins/wasm-go/extensions/ai-statistics/plugin.wasm`，最后重启 `deployment/higress-gateway`。只 `kubectl apply` `WasmPlugin` YAML 不会刷新 wasm 二进制。
- `ai_log` 默认会带 `request_body`、`response_body`。`/v1/responses` 非流式响应里常见的 `instructions`、`tools` 也可能进入 `response_body`，日志会明显变大；如果本地只想看摘要，可以在 `defaultConfig` 里额外加 `response_body_length_limit`。
- `/v1/responses` 流式场景里，最终日志中的 `response_body` 不是原始 SSE event 列表，而是插件聚合后的最终对象，目标是尽量接近非流式响应；如果你要从原始事件级字段取值，继续使用 `response_streaming_body`。
- 排查时优先看 access log 里的 `ai_log`，不要只看 wasm stdout。`ai_log` 没内容时，先检查 `matchRules.ingress`、最新 `plugin.wasm` 是否已经拷进 kind 节点，以及网关是否已经 `rollout restart`。

当前本地生效配置可以这样查看：

```bash
kubectl -n higress-system get mcpbridge -o yaml
kubectl -n higress-system get ingress bedrock-local -o yaml
kubectl -n higress-system get ingress openai-local -o yaml
kubectl -n higress-system get wasmplugin ai-proxy-local -o yaml
kubectl -n higress-system get wasmplugin ai-statistics-local -o yaml
```

## 8. 绑定本地 8080 端口

把 Higress 网关端口转发到本地：

```bash
kubectl -n higress-system port-forward deployment/higress-gateway 8080:80
```

成功后，本地统一入口就是：

```text
http://127.0.0.1:8080
```

如果 `8080` 被旧的 `kubectl port-forward` 占用，可以先结束旧进程，或者改成：

```bash
kubectl -n higress-system port-forward deployment/higress-gateway 18080:80
```

## 9. 查看日志

最快的看法是直接按 label 看网关容器滚动日志：

查看滚动日志：

```bash
kubectl -n higress-system logs -l app=higress-gateway -c higress-gateway -f
```

如果你想先带出最近 `N` 行，再持续跟随：

```bash
kubectl -n higress-system logs -l app=higress-gateway -c higress-gateway --tail=200 -f
```

如果你想先确认当前 pod，再做更细的筛选，可以按下面两步：

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
kubectl -n higress-system rollout status deployment/higress-gateway --timeout=180s
```

## 10. curl 调用示例

### 10.0 Bedrock `/v1/messages` 原生透传说明

`bedrock.local` 下如果请求的是 Claude 协议 `/v1/messages`，当前实现统一走 Bedrock 原生 Claude Messages 路径，不再转换成 OpenAI `chat/completions`。

当前行为：

- 非流式 `/v1/messages` 使用 `InvokeModel`
- 流式 `/v1/messages` 使用 `InvokeModelWithResponseStream`
- 最终请求路径优先基于 `modelMapping` 后的真实 Bedrock model ID / inference profile ID 构造

例如：

- `claude-sonnet-4-6` 映射为 `global.anthropic.claude-sonnet-4-6`
- 流式 `/v1/messages` 会改写为 `/model/global.anthropic.claude-sonnet-4-6/invoke-with-response-stream`
- 非流式 `/v1/messages` 会改写为 `/model/global.anthropic.claude-sonnet-4-6/invoke`

如果某个 Bedrock model ID 本身不支持原生 Claude Messages，请以上游返回结果为准。

### 10.1 基础 Bedrock 调用

```bash
curl http://127.0.0.1:8080/v1/messages \
  -H 'Host: bedrock.local' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "claude-sonnet-4-5",
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
    "model": "claude-sonnet-4-5",
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
    "model": "claude-sonnet-4-5",
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

### 10.4 `chat_completions` / `responses` / `messages` 的 cache usage 对比

这三种契约都可能表达“缓存创建”和“缓存读取”，但字段口径不同，不能混着解释。

#### `chat_completions`

常见字段：

- `usage.prompt_tokens`
- `usage.completion_tokens`
- `usage.total_tokens`
- `usage.prompt_tokens_details.cached_tokens`
- `usage.cache_creation_input_tokens`
- `usage.cache_read_input_tokens`

建议解释口径：

- `prompt_tokens`：总输入 token，通常已经包含 cache read
- `completion_tokens`：输出 token
- `prompt_tokens_details.cached_tokens`：命中的缓存输入
- `cache_creation_input_tokens`：本次新创建进缓存的输入

如果上游同时返回了：

- `prompt_tokens`
- `cache_creation_input_tokens`
- `cache_read_input_tokens`

那更稳妥的理解是：

- 总输入 token = `prompt_tokens + cache_creation_input_tokens`
- 其中命中缓存部分 = `cache_read_input_tokens` 或 `prompt_tokens_details.cached_tokens`

#### `responses`

常见字段：

- `usage.input_tokens`
- `usage.output_tokens`
- `usage.total_tokens`
- `usage.input_tokens_details.cached_tokens`
- 部分实现也会额外带 `usage.cache_creation_input_tokens`
- 部分实现也会额外带 `usage.cache_read_input_tokens`

建议解释口径：

- `input_tokens`：输入 token 主口径
- `output_tokens`：输出 token 主口径
- `input_tokens_details.cached_tokens`：命中的缓存输入
- 如果额外返回了 `cache_creation_input_tokens` / `cache_read_input_tokens`，则它们是对 input 侧缓存细分的补充字段

`responses` 契约下不要默认套用 `messages` 的总 token 公式，优先以响应里显式给出的 `total_tokens` 为准。

#### `messages`

常见字段：

- `usage.input_tokens`
- `usage.output_tokens`
- `usage.cache_creation_input_tokens`
- `usage.cache_read_input_tokens`
- `usage.cache_creation.ephemeral_5m_input_tokens`

`messages` 契约建议按下面公式理解：

```text
total_input_tokens =
input_tokens
+ cache_creation_input_tokens
+ cache_read_input_tokens
```

```text
total_tokens =
input_tokens
+ cache_creation_input_tokens
+ cache_read_input_tokens
+ output_tokens
```

也就是：

- `input_tokens`：本次实际新算的非缓存输入
- `cache_creation_input_tokens`：本次新创建缓存的输入
- `cache_read_input_tokens`：本次命中缓存读取的输入
- `output_tokens`：输出

如果同时存在：

- `cache_creation_input_tokens`
- `cache_creation.ephemeral_5m_input_tokens`

优先把它们视为同一语义的不同表达，优先读显式顶层字段。

### 10.5 `anthropic/v1/messages` capability 为什么有时还能看到原始 `event:`

排查 OpenAI provider 暴露 `anthropic/v1/messages` capability 时，不能只看请求侧有没有走 OpenAI 逻辑，还要看响应体有没有真的被读取和重写。

当前代码里有一个很关键的现象：

- 如果 `ApiNameAnthropicMessages` 在 `openaiProvider.TransformRequestBody(...)` 里没有提前返回
- 请求会继续走 `transformRequestFields(...)`
- 但 `transformRequestFields(...)` 里并没有 `ApiNameAnthropicMessages` 的专门处理分支
- 因此局部变量 `needReadResponseBody` 会保持为 `false`
- defer 最后会执行 `ctx.DontReadResponseBody()`

这会直接导致：

- `onStreamingResponseBody(...)` 不会接管这条响应
- 不会进入统一的 `ExtractStreamingEvents(...) -> ToHttpString()` 回写链路
- 上游原始 SSE 会被直接透传

因此如果你在本地对 `Host: kimi.local` 之类的 capability 链路实测时仍然看到：

```text
event: message_start
event: content_block_delta
event: message_delta
event: message_stop
```

这不一定说明本地 `ToHttpString()` 保留了 `event:`，更常见的原因是：

- 这条响应根本没被读 body
- 上游 Anthropic/Messages 风格 SSE 被原样透传了

反过来说，如果把 `ApiNameAnthropicMessages` 提前从 `TransformRequestBody(...)` 返回，跳过 `transformRequestFields(...)`，那就不会再依赖这条隐式的 `DontReadResponseBody()` 透传行为，后续是否保留 `event:` 要看实际响应处理分支。

### 10.6 fine-grained tool streaming 调试示例

通过 OpenAI 协议里的 `extra_headers.anthropic-beta` 开启：

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Host: bedrock.local' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "claude-sonnet-4-5",
    "stream": true,
    "extra_headers": {
      "anthropic-beta": "fine-grained-tool-streaming-2025-05-14"
    },
    "tools": [
      {
        "type": "function",
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

- 接收 OpenAI body 里的：

```json
"extra_headers": {
  "anthropic-beta": "fine-grained-tool-streaming-2025-05-14"
}
```

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

- `local/envoy.local.yaml`
- `local/envoy-bedrock.yaml`

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

## 12. FAT 发布流程

代码开发完成后，如果需要把插件发布到 FAT 环境，当前使用的流程如下。

TCS 权限与 RBAC 相关说明参考：

- `https://pages.release.ctripcorp.com/sysdev-docs/container-platform/docs/tcs/userguide/user-rbac`

### 12.1 更新 ai-gateway-plugin-server

先在本地把对应插件的 wasm 打好。

然后打开：

- `https://git.dev.sh.ctripcorp.com/framework/ai-gateway-plugin-server`

处理步骤：

1. 替换 `plugins/` 目录下对应插件产物。
2. 修改对应插件的 metadata，把版本标识改成 `ai-gateway` 项目里的当前 commit id。
3. push 到远端。

### 12.2 生成插件镜像

如果有多人在不同分支开发 `ai-proxy` 的不同功能，但这些功能需要一起发 NTZ / FAT 验证，当前做法是把这些功能分支都配置到 `light merge`。

配置完成后，这些分支的变更会被合并到 `dev` 分支。

因此实际打包时，不是直接在各自功能分支上打，而是：

1. 切到 `dev` 分支。
2. 确认 `dev` 已经包含当前配置到 `light merge` 的多个分支合并结果。
3. 在本地基于 `dev` 分支代码打插件。
4. 再到插件仓库 pipeline 中找到 `buildimage` 任务，拿到构建出的镜像 tag。

换句话说，这一步的 `dev` 分支代码，是一个联调基线，包含了多个已经配置进 `light merge` 的功能分支改动。

常见镜像 tag 形态：

- `dev-f5b068ba-20260415211115`
- `release-xxxx-xxxxxx`

其中：

- `dev-*` 通常用于测试环境
- `release-*` 通常用于生产环境

### 12.3 更新 configs-infra-v2

打开：

- `https://git.dev.sh.ctripcorp.com/cloudnative/configs-infra-v2/-/pipelines/54997104`

然后在本地处理：

1. 拉取 `master` 最新代码。
2. 基于最新 `master` checkout 一个新的工作分支。
3. 找到 `components/ai-gateway/values.yaml`。
4. 把镜像 tag 改成上一步 `buildimage` 产出的镜像 id。
5. commit 这个配置变更。

当前本地参考路径：

- `/Users/jkma/programfiles/codinghere/ailabgo/configs-infra-v2/components/ai-gateway/values.yaml`

### 12.4 打 tag 并触发发布

在 `configs-infra-v2` 仓库基于刚才这次配置变更打发布 tag。

tag 命名示例：

- `ai-gateway-v20260416-beta-2`

然后 push tag。

接着到 `configs-infra-v2` 的 pipeline 中生成测试环境发布计划，并发布到 `-z` 集群。

### 12.5 重启 FAT `-z` 集群网关

配置发布完成后，还需要在 FAT `-z` 集群上手动重启对应 workload，确保拉取到新的镜像。

实际操作按当时环境里的 k8s 资源名执行，核心目标是：

- 让 `-z` 集群网关重新拉取新镜像
- 不要只停留在配置已发布但 Pod 未重建的状态

当前这套环境可以直接按下面步骤执行：

1. 切换到 FAT `-z` 集群：

```bash
tdkcu ntgxh-x
```

2. 查看 Pod：

```bash
tdk get pod -n fat-ai-gateway
```

3. 查看 Pod 和 IP：

```bash
tdk get pod -n fat-ai-gateway -o wide
```

4. 滚动重启 `ai-gateway`：

```bash
tdk rollout restart deployment/ai-gateway -n fat-ai-gateway
```

5. 查看重启状态：

```bash
tdk rollout status deployment/ai-gateway -n fat-ai-gateway
```

如果这里只是验证镜像是否已经重新拉取，通常第 `3` 步和第 `5` 步最关键。

### 12.6 FAT Pod 日志与 OOM 排查

如果要看挂掉 Pod 的日志，先找到当前 `ai-gateway` Pod：

```bash
tdk get pod -n fat-ai-gateway | grep ai-gateway
```

查看当前 Pod 日志：

```bash
tdk logs <pod名> -n fat-ai-gateway
```

查看上一个已经退出的容器日志，适合排查 OOM 或启动后崩溃：

```bash
tdk logs <pod名> -n fat-ai-gateway -p
```

如果直接按 deployment 查看上一轮日志：

```bash
tdk logs deployment/ai-gateway -n fat-ai-gateway -p
```

常用过滤参数：

```bash
tdk logs <pod名> -n fat-ai-gateway --tail=200
tdk logs <pod名> -n fat-ai-gateway --since=1h
```

确认 Pod 是否发生过 OOMKilled：

```bash
tdk describe pod <pod名> -n fat-ai-gateway | grep -A5 -i "last state\|terminated\|reason"
```

如果命中 OOM，通常会看到 `Reason: OOMKilled` 或 `Exit Code: 137`。

## 13. 常见问题

### 13.1 `route_not_found`

通常是 host 不对。

比如：

- `Host: anthropic.local` 但本地没配这条 ingress

当前本文档的主路径只保证：

- `Host: bedrock.local`

### 13.2 请求打通了，但没有缓存命中字段

优先排查：

- 请求前缀太短
- `cache_control` 打在太靠后的块上
- 模型或 profile 没返回 cache usage

不是只要带了 `cache_control` 就一定会看到缓存 token 字段。

### 13.3 access log 里 `ai_log` 是 `"-"`

表示当前请求没有往 `wasm.ai_log` 写内容，不代表插件没生效。

### 13.4 看到 access log，但看不到 wasm 业务日志

优先检查：

- 网关日志级别是否调高
- 当前请求是否真的命中 ai-proxy
- 是否只是在看 access log，而不是完整容器日志

### 13.5 `No rule to make target help`

这个仓库的 `make` 目标不是统一提供 `help`，直接用明确的命令即可，不需要先跑 `make help`。
