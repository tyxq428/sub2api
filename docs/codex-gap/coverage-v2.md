# 第二批代理绑定覆盖矩阵

本批从 `42798052390086a1b012f3eed05f2ab5513933b9` 开始，在 `feature/codex-gap-v1` 实施。原始官方基线仍为 v0.2.5 / `86f93c28ee34cc74b629dafb748bd5ac5ca8c5ea`。这里只说明局部出站正确性，不宣称风控概率、账号质量或完整协议指纹已经改变。未推送或部署。

## 已接入并验证的边界

| ID | 入口/边界 | 修复后的约束 | 证据与限制 |
|---|---|---|---|
| B2-01 | OAuth GenerateAuthURL | 明确 ProxyID 缺失、查询失败、ID不符或地址无效时，创建授权会话前拒绝 | 5类故障×authorize；合法绑定保留原授权流程 |
| B2-02 | OAuth ExchangeCode | 显式代理覆盖失败不再回退旧 session 代理；未指定覆盖时校验 session URL | 状态校验仍优先；错误不消费 session；成功仍消费一次 |
| B2-03 | RefreshTokenWithClientID | 原始代理 URL 非法时，调用 OAuth client 前拒绝，错误不含认证信息 | URL快照验证，不是数据库绑定重新查询 |
| B2-04 | RefreshAccountToken | 账号明确绑定必须从 proxyRepo 成功读取后，才进入 refresh/PAT/现存token enrichment 分支 | 有repo时保持最新记录优先，拒绝缺失repo；RT与现存access分支有新红绿测，PAT既有测试通过 |
| B2-05 | Admin Ensure/ForceOpenAIPrivacy | 绑定失败不调用隐私网络工厂；记录 failed，不误记为 training_off | 已成功时 ensure 仍跳过、force仍执行；故障可在以后重试 |
| B2-06 | TokenRefreshService.ensureOpenAIPrivacy | 后台隐私检查同样失败关闭，并保留失败重试状态 | 与token refresh本身分开测试；未改变隐私设置目标 |
| B2-07 | FetchCodexModelsManifest | 在认证头、缓存命中、网络工厂前校验；shadow 使用母账号凭据及母账号代理 | 本地目标＋本地HTTP代理验证绝对形式请求确实进入母账号代理；清空绑定后不能靠缓存绕过 |
| B2-08 | OpenAIGatewayService.GetAccessToken | 已选OpenAI账号缺少明确绑定时，在token cache/refresh前拒绝 | OAuth/setup-token/API-key三类；shadow先解析母账号；其他平台不受新增策略影响 |
| B2-09 | doOpenAIUpstream / doOpenAIAccountTestUpstream | 插件交接和HTTP transport之前，以明确账号绑定为准；错误不传给下游transport | fake transport验证请求体、UA不变及错误时零dispatch；插件前顺序经源码核对，不宣称插件自身遵守代理 |
| B2-10 | WS pool Acquire | 检查绑定后才能复用已有socket；同账号更换代理不能借到旧代理连接 | 同路由复用、缺失绑定拒绝、更换地址新建连接均通过 |
| B2-11 | WS pool dialConn（含prewarm） | 代理失败先于HeadersFactory和Dial | fault injection验证认证头回调与拨号均不执行 |
| B2-12 | WS请求克隆与兼容键 | 复制Account中的ProxyID/Proxy值；兼容键加入规范化代理URL摘要 | 原对象变更不污染已捕获路由；兼容键不保存明文代理凭据；不等于完整Account深拷贝 |

上述公共边界保护经过它们的主HTTP、部分WS和辅助调用，不能仅凭函数名宣称所有端点已覆盖。

## 明确保留的行为

没有绑定的账号仍使用原默认/显式候选路由，不新增禁止环境代理的全局策略。代理过期、状态、管理员配置的direct fallback及数据库解绑政策不改。UA/originator/version、session/thread/window、TLS/HTTP2/WS帧、响应解析、调度和模型内容均未重写。

错误码：绑定不可用为 `OPENAI_PROXY_UNAVAILABLE`，无效地址为 `OPENAI_PROXY_INVALID`，均502；OAuth旧“找不到代理”的部分400会变为502。Privacy维持其既有字符串结果API，写入 `PrivacyModeFailed`，不是通过返回502表达失败。

WS兼容键只判断配置的代理URL，不判断代理服务器最终公网出口是否轮换；摘要也不是网络指纹。已有会话必须使用指定旧连接时，路由不符仍沿用现有“不允许漂移”错误，而非偷偷换通道。

## 尚未独立覆盖的范围

| 项目 | 当前状态 / 不得做出的承诺 |
|---|---|
| Dedicated WS v2 passthrough | 不经过池的专用dialer尚未新增独立故障注入；可能经GetAccessToken受保护，不能据此把整个专用通道打勾 |
| Agent Identity task registration | `registerAgentIdentityTask` 独立网络入口未本批改造；外层部分调用已有前置校验不等于完整覆盖 |
| live/alpha search/独立图片与下载/legacy bridge | 仍需逐调用栈确认是否绕开本批公共边界，不能以文件名属于OpenAI就认定覆盖 |
| 第三方插件内部网络 | 本批检查的是交给插件之前的绑定及参数；插件自己的HTTP栈须另验 |
| 即时撤销与并发配置变更 | eager-loaded账号使用请求快照，不每次查询最新数据库。WS只复制路由相关字段，其他metadata/credentials仍按既有生命周期处理 |
| OAuth会话保存的URL | 未显式传新的ProxyID时继续使用授权时URL快照；没有增加持久化ProxyID及每次重新验证撤销状态 |
| 重定向、实际出口IP、DNS/IPv6 | 本批不宣称全路径无重定向泄露或实际出口一致；本地模拟HTTP代理不等于真实CONNECT/TLS/VPS验收 |
| Gin/Nginx整条入口与用户状态 | 本批不是全入口抓包、生产验收、上游账号风险实验或完整不可变attempt实现 |

## 主要代码定位

文件均位于 `backend/internal/service/`，下方行号对应本批提交前最终代码。

- `openai_outbound_proxy.go:19`：`resolveOpenAIAccountProxyURL`
- `openai_outbound_proxy.go:65`：`resolveOpenAIProxyIDURL`
- `openai_outbound_proxy.go:71`：`validateOpenAIProxyURL`
- `openai_outbound_proxy.go:82`：`resolveOpenAIDispatchProxyURL`
- `openai_outbound_proxy.go:91`：`openAIProxyBindingHash`
- `openai_oauth_service.go:45`：`(s *OpenAIOAuthService) GenerateAuthURL`
- `openai_oauth_service.go:127`：`(s *OpenAIOAuthService) ExchangeCode`
- `openai_oauth_service.go:211`：`(s *OpenAIOAuthService) RefreshTokenWithClientID`
- `openai_oauth_service.go:338`：`(s *OpenAIOAuthService) RefreshAccountToken`
- `admin_account.go:1635`：`(s *adminServiceImpl) EnsureOpenAIPrivacy`
- `admin_account.go:1670`：`(s *adminServiceImpl) ForceOpenAIPrivacy`
- `openai_codex_models_service.go:1625`：`(s *OpenAIGatewayService) FetchCodexModelsManifest`
- `openai_gateway_service.go:1202`：`(s *OpenAIGatewayService) GetAccessToken`
- `openai_plugin_transport.go:11`：`(s *OpenAIGatewayService) doOpenAIUpstream`
- `openai_plugin_transport.go:35`：`(s *AccountTestService) doOpenAIAccountTestUpstream`
- `openai_ws_pool.go:1110`：`(p *openAIWSConnPool) Acquire`
- `openai_ws_pool.go:2125`：`(p *openAIWSConnPool) dialConn`
- `openai_ws_pool.go:2331`：`cloneOpenAIWSAcquireRequest(`
- `openai_ws_pool.go:2393`：`normalizeOpenAIWSHandshakeCompatibility`
- `token_refresh_service.go:1443`：`(s *TokenRefreshService) ensureOpenAIPrivacy`

测试与证据见 [verification-v2.md](verification-v2.md)。第一批矩阵继续保留作为基线说明；本页覆盖状态更新其 G09，不撤销已列出的待验证事项。
