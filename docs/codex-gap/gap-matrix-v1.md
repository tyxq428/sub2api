# Codex 出站一致性 Gap Matrix · v1

## 1. 范围、基线与判定口径

本文件是首批最小改动的开发验收矩阵，不是上游风控评分、封号率或模型质量的测量报告。目标是消除可验证的出站路由错误和信息泄露，避免未经验证的身份重写。不能由“与官方客户端不同”直接推导为“异常/降智原因”。

- Sub2API 唯一代码基线：`v0.2.5` / `86f93c28ee34cc74b629dafb748bd5ac5ca8c5ea`。
- 官方 Codex 参考：`rust-v0.154.0` / commit `6b9826e3aa83b1a5947db50f4332cb9c65f1b340`。annotated tag 对象是 `36eab01061df3cde5f95ec20a526777b430091ba`，它不是源码 commit；GitHub 返回该 tag unsigned，不宣称签名验证。
- 源仓库：`E:\Apps\sub2api`；独立开发目录：`E:\Apps\sub2api-worktrees\codex-gap-v1`；分支：`feature/codex-gap-v1`。
- 参考账号：用户指定账号 8。此前对话中的同代理登录/出口/参考样本属于历史运行证据，本轮未重放、未重新抓包，也未将旧摘要当成本次测试通过证据。
- 本轮：只改独立本地分支；不改 VPS、账号、代理、数据库、生产镜像、系统时区；不做真实上游调用或 MITM。
- 下列源码行号针对**未修改的基线 commit**；修改后以函数名及 Git diff 定位。

证据等级：**S**=当前基线源码确认；**T**=本轮离线可重复测试；**H**=历史运行记录、未在本轮重验；**Q**=待验证假设。S/T 不代表上游检测概率已量化。状态分为：已具备、条件性缺陷、待验证、明确差异但非独立缺陷、首批修复。

## 2. 正式矩阵

| ID | 项目与最终判定 | 基线证据 / 等级 | 最小动作 / 验收 | 本批处置 |
|---|---|---|---|---|
| G01 | 默认 UA 含固定 Ubuntu 模板；**不是动态环境探测**，但支持管理员覆盖 | `openai_gateway_service.go:40–44`；`openai_codex_identity.go:133–161`；S | 先测现有 override 能否表达目标；不把宿主/容器/代理地理信息自动混成一个 UA | 保留默认；加 exec override 特征测试 |
| G02 | 指纹收敛 off 与 identity enforcement 是独立开关；off 不等于所有字段透传 | `openai_codex_fingerprint.go:194–209,277–284`；`openai_codex_identity.go:222–249`；S | 分别记录两层策略，避免用户把 off 当完整透明代理；配置说明＋特征测试 | 文档及测试，不改开关 |
| G03 | “不支持 exec / 所有请求强制变 TUI”是过强结论；已有候选 UA 配对和覆盖 | `openai_codex_identity.go:140–161,222–234`；已有 override 测试；S | 通过 `codex_exec` 候选和 canonical version 验证 UA/originator/version；复用已有函数 | 不新增平行身份系统 |
| G04 | 普通 HTTP allowlist 缺少部分连字符别名，但**不等于最终出站一定缺头** | `openai_gateway_service.go:74–110`；`openai_gateway_forward.go` 后续重写；S/Q | HTTP/WS/compact 真入口→fake upstream 比较别名归一化和 namespace，再确定修复 | 待完整入口复现；不裸透传 |
| G05 | session/full 的派生生命周期与官方参考可能不同；账号 8 历史为 off，不应称已触发 | `openai_codex_fingerprint.go:212–257,282–315`；S/H/Q | 按 mode、root/subagent、turn/window 关系测试；先保留 off；不全局固定或随机轮换 ID | 不改生命周期 |
| G06 | 已有 identity 三元组和部分共享结果；“完全没有 snapshot”不准确 | `openai_codex_identity.go:124–161`；fingerprint 注释要求头/体共用一次结果；S/Q | 用热更新/重试并发测试确认跨组件快照边界，再考虑扩展不可变 attempt | 架构方案，不做大重构 |
| G07 | **明确绑定代理但未 eager-load、补查缺失/失败时，quota preflight 成功返回空 URL** | `openai_quota_service.go:404–473`，尤其 `462–470`；S；回归 T | 统一小型 resolver；绑定无法满足→结构化 502；在 token 获取前验证；query/reset/targeted reset 工厂调用数=0 | **首批修复：仅 quota 家族** |
| G08 | **代理 URL 解析失败把原始 URL 经 `%v` 放进错误；Redacted 仍可保留用户名/query** | `internal/pkg/proxyurl/parse.go:42–54`；S；带合成凭据的解析错误回归 T | 静态安全错误；不输出原始 URL、userinfo、query、底层 URL error；原有效协议保持不变 | **首批修复：共享 parser 错误脱敏** |
| G09 | 其他 HTTP/WS/OAuth/模型/隐私路径的严格绑定覆盖未完成；不是“全站已证明漏 IP” | `openai_gateway_forward.go:1061–1063`；`openai_oauth_service.go` 等；S/Q | 后续逐路径接入 resolver 前补各自契约测试；禁止把 quota 修复宣传为全链路 fail-closed | 明确遗留范围 |
| G10 | quota/privacy、认证、推理客户端与 header 不同；**不同本身不必然是 bug** | `openai_quota_service.go:25–37,114–116,563–579`；`repository/req_client_pool.go:58,96–99`；S | 每类端点比较自己的官方/协议要求；不把 quota 全改成 exec 身份，不改变隐私设置 | 继续审计，不统一抹平 |
| G11 | 已存在 previous-response/account sticky、busy保持等保护；“没有 continuation 绑定”不成立 | `openai_ws_account_sticky_test.go:12,51,91,282,348`；S/Q | 针对限流/排除/未知 owner 的后续行为补 reproduction；不能从缓存 miss 直接推断跨账号实际重放 | 不改调度与续链 |
| G12 | 已有大量线协议、转发与 WS reader tests；缺的是特定失败边界的覆盖证明 | `openai_ws_pool_reader_loop_test.go`；`openai_quota_spark_window_test.go`；S | 新增精确故障注入、服务→fake upstream、required test run/pass gate；明确非完整 ingress 验证 | **新增定向回归及运行门禁** |
| G13 | 官方参考缺少本项目固化的可复核锁定材料；不是网络故障 | 官方 tag/commit API；本地 SHA；S | 写入参考锁；升级时显式审阅敏感文件。完整上游 drift CI 后续做，不将静态 lock 冒充已启用 gate | 首批固定 lock＋来源 |
| G14 | TLS/H2/WS 与原生的差异需要按端点测量；历史候选 JA3不能证明全路径对应 | 历史摘要 H；当前无完整重放 Q | 区分原生握手与 MITM；版本、ALPN、H2、WS 控制帧分别测；不套单一 JA3 | 不改 transport |
| G15 | 宿主、容器、代理地理时区不同不自动构成错误；IP不强制决定客户端时区 | 历史环境 H；是否发送相关字段 Q | 只检查真正发出的字段与真实执行语义；保留远端 Windows workspace 等真实信息；不造城市/attestation | 不改 TZ/locale/正文 |
| G16 | zstd / WS 压缩条件差异未按本基线与官方路由跑完；不是已确认缺失 | 需 encoder/Content-Encoding/长度/重试的完整对照；Q | 固定端点/auth/transport 做解压语义与编码策略测试再改；不全局开启压缩 | 后续独立小批次 |
| G17 | JSON 重序列化可能改变字节，但字节变化≠已证实检测风险 | 需原始与转发 body 对照；Q | 先保障未知字段、数值、工具参数语义；能不改则不改，不能为了字节一致破坏规范化安全性 | 待取证 |
| G18 | Nginx 下划线头是否丢失尚未现场复现 | Q | 只对无凭据的受控请求验证 proxy→handler；未验证前不调整生产 Nginx | 待验证 |
| G19 | 内部 `sub2api:` namespace 经摘要派生 UUID，不等于 Header 明文品牌泄露 | `openai_codex_fingerprint.go:212–257`；S | 保留内部命名；检查实际出站值而非全局替换项目名；任何“完全没有泄露”结论限于已测路径 | 不做全局去品牌 |
| G20 | 代理到期 direct fallback 是单独可配置策略，不等于错误解析；未绑定账号可保留原默认路由 | `proxy_fallback.go:5–47`；S | 本批不改 expiry/status/fallback 政策；只拒绝请求开始时未满足的明确绑定 | 保持既有授权策略 |

路径未写完整前缀的 service 文件位于 `backend/internal/service/`。

## 3. 首批边界与验收合同

### 3.1 quota 代理绑定

`QueryUsage / ResetCredit / ResetCreditTargeted → prepareUpstreamCall → resolveOpenAIAccountProxyURL`。

没有 ProxyID：保留原有空 URL/客户端默认路由语义（不是新增“禁止环境代理”承诺）。有 ProxyID：要求有效正 ID、匹配的已加载或补查代理、有效 host/port/protocol。解析失败必须在 token 获取和 client factory 前返回，不允许借失败变为默认路由。影子账号以解析后的母账号绑定为准。resolver 不写数据库、不修改账号/Proxy 对象、不改自动到期迁移策略。

本批只接入 quota 家族；并不保证并发配置变化、未迁移的 OAuth refresh 或其他服务路径已有全局不可变快照。这些必须独立验收。

### 3.2 错误日志最小化

无效 proxy URL 的错误只给稳定原因，不返回 URL、user/password/query 或可 Unwrap 到原始 URL 的错误。测试中的凭据全是固定合成字符串；不读取生产账号。

### 3.3 验收不是 exit 0

必须保存 baseline red 与候选 green；Go 的编译失败不能冒充红测通过；`go test -json` 中 required test 必须出现 `run` 和 `pass`，不能被 build tag、筛选、skip 或缓存悄悄省略。使用 `-count=1`、`-mod=readonly`。保留原 parser、shadow、identity 特征测试。独立 runner 只运行本地 fake 服务，不触碰真实上游。

## 4. 后续最小批次，不在本次一并重写

1. 将严格绑定逐路径扩展到 OAuth、models、privacy、主 HTTP/WS；各路径先建立红测，明确凭据刷新与账号快照边界。
2. 验证现有 canonical UA/override 是否满足参考配置，补 HTTP/WS/compact 真入口别名/namespace 合同；只有实际失败时再改 identity projection。
3. 按 mode 测生命周期与 continuation 边界；不强制开启 convergence，不随意丢弃 encrypted/turn-state。
4. 独立审计 zstd 与 transport、Nginx；不以同语言、单个 JA3 或代理 IP 相同代替实测。
5. 生产部署为独立交付：固定自有镜像 digest、备份、回滚、灰度与请求终态验收；本批不部署。

## 5. 来源与复核方式

- [Sub2API v0.2.5 基线](https://github.com/Wei-Shaw/sub2api/tree/86f93c28ee34cc74b629dafb748bd5ac5ca8c5ea)
- [quota preflight](https://github.com/Wei-Shaw/sub2api/blob/86f93c28ee34cc74b629dafb748bd5ac5ca8c5ea/backend/internal/service/openai_quota_service.go#L404-L473)
- [proxy URL parser](https://github.com/Wei-Shaw/sub2api/blob/86f93c28ee34cc74b629dafb748bd5ac5ca8c5ea/backend/internal/pkg/proxyurl/parse.go#L36-L65)
- [existing identity resolver](https://github.com/Wei-Shaw/sub2api/blob/86f93c28ee34cc74b629dafb748bd5ac5ca8c5ea/backend/internal/service/openai_codex_identity.go#L124-L249)
- [官方 Codex UA/default client](https://github.com/openai/codex/blob/6b9826e3aa83b1a5947db50f4332cb9c65f1b340/codex-rs/login/src/auth/default_client.rs)
- [官方 tag 元数据](https://api.github.com/repos/openai/codex/git/tags/36eab01061df3cde5f95ec20a526777b430091ba)

外部 fork 仅作为设计灵感，首批不复制其代码，也不将其“降低风控/恢复账号”叙述当作实验结论。

测试结果及交付 commit 记录在同目录 `verification.md`；未写入通过结果前，不视为完成。

## 第二批状态更新

第二批已在首批提交之上扩展 OAuth、models、admin/background privacy、公共HTTP和WS池边界。G09由“仅quota接入”更新为“部分主要边界已接入、专用通道仍待验证”；G06仅增加WS路由字段的值快照，不宣称完整不可变attempt。新增发现是WS旧连接可能跨代理配置复用，已通过路由摘要兼容键和回归修复。详细覆盖、错误码兼容性和剩余范围见 [coverage-v2.md](coverage-v2.md)，测试见 [verification-v2.md](verification-v2.md)。
