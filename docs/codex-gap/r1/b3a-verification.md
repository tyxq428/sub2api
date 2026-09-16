# R1 · B3a 管理入口与 OAuth 会话代理绑定

基线 `abe449690bda37ebaf68485535358daec0e99d34`。本批未触碰生产、凭据值、数据库结构或客户端身份字段。

## 缺陷与修复

1. 管理员 RefreshToken handler 在 GetProxy 返回错误或 nil 时仍向服务传递空地址。已改为 handler 使用与 service 相同的绑定校验；错误固定脱敏，明确指定但无法满足的绑定返回502，不调用 OAuth client。同一 helper 接到 PAT 创建入口，PAT 底层认证逻辑未改。
2. OAuth 授权 session 只记录 URL，代理已删除或改变后仍可用旧 URL 交换授权码。现在内存 session 独立保存原始 ProxyID；未传显式新代理时重新查绑定，路由变更返回 OPENAI_PROXY_BINDING_CHANGED，错误不消费 session。显式 override 仍保留原既有语义；旧的未记录ID会话仍为明确URL快照。
3. 保存的ID按值复制，不受请求调用方之后修改原指针影响。

## 证据

- 原实现编译成功后：3个主测试、7个子测试失败；正常/未绑定对照通过。红测 job `job_V9kYJ_lJGuSuu9oqU4Vb`。
- 候选与既有OAuth/session回归：18个主测试、35个子测试通过；四个新主测试全部实际run/pass。绿测 job `job_zZCvmrX4cEAHGIvkWvlT`。
- 真 Gin 路由用于 refresh handler；上游是不能联网的合成客户端。PAT入口复用已测helper，不冒充PAT端到端新验收。
- 无数据库migration：新增字段仅属于内存授权session。禁用/到期、旧会话撤销及完整在途撤销仍列入B3后续，不宣称此提交解决全链路撤销。

- `b3-red-auth.jsonl` SHA256 `e09606a285e3136e7ab09be41797317c477c31a58fab4f73f75ae50c0ffb876e`
- `b3-green-auth.jsonl` SHA256 `b0175797c4c30a25b0804b5b82e09ed0217449e2a8d6a806b472dd237bf2907e`
