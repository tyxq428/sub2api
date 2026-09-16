# 首批最小补丁验收记录

## 交付范围

- 基线：Sub2API v0.2.5 / `86f93c28ee34cc74b629dafb748bd5ac5ca8c5ea`。
- 分支：`feature/codex-gap-v1`。
- 本批修复：G07 quota 代理绑定错误降级；G08 proxy URL 错误信息泄露。另补身份现状特征测试、必跑测试门禁及参考锁。
- 本文件与补丁一起提交。查看实际交付提交：`git log -1 --format=%H -- docs/codex-gap/verification.md`。
- 未合并主分支、未 push、未部署 VPS、未修改账号 8 或其他生产配置。

## 验证结果

| 检查 | 结果 | 说明 |
|---|---|---|
| 原版 parser 红测 | 已复现 | 编译成功后，6 个错误场景中 5 个会暴露合成凭据标记；不是编译失败冒充红测 |
| 原版 quota 红测 | 已复现 | 30 个绑定故障子场景失败；另有校验先于 token、影子母账号绑定两个主测试失败 |
| 修复后定向 Go 测试 | **33 主测试＋69 子测试通过** | parser + service；无失败；18/18 必跑测试有 run 和 pass，无缺失/skip |
| 共享 proxyutil 回归 | **15 主测试＋20 子测试通过** | 保留 HTTP/HTTPS/SOCKS 的既有下游行为 |
| 合计 | **48 主测试＋89 子测试通过** | 这是定向回归，不是全仓库测试 |
| 测试门禁自测 | 通过 | 空日志、skip、无 run 只有 pass、Go 非零退出、package build failure 均拒绝 |
| Linux amd64 后端交叉编译 | 通过 | Go 1.27.1、CGO_ENABLED=0、无前端 embed；不是完整 Docker 镜像或 Linux 上运行验收 |
| git diff --check | 通过 | 最终提交前再次验证 |
| main 工作区 | 保持干净 | `E:\Apps\sub2api` 未增加业务改动；原有 ahead 2 未推送状态保留 |

## 执行环境

系统原 Go 为 1.17.6，不满足 backend/go.mod 的 1.27.0。使用独立官方 Go 1.27.1 SDK：

`D:\Temp\sub2api-build\go1.27.1\go`

下载包 SHA-256：`a3911b5e0e1b1053f25ed0675f4c1c6aad1e2bfcf253df2b9be4caabd2edd95d`，已核对。没有修改系统 Go、全局 PATH 或 GOROOT。使用进程级缓存与工具链环境，`GOTOOLCHAIN=local`、`GOWORK=off`、`-mod=readonly`，保留模块校验。

## 可重放命令

在具备 go.mod 所要求 Go 版本的开发环境中：

```text
python backend/scripts/check-codex-gap-tests.py --self-test
python backend/scripts/check-codex-gap-tests.py --go <go-executable> --log <local-jsonl-path>
```

也可只验证既有测试日志：

```text
python backend/scripts/check-codex-gap-tests.py --verify-log <local-jsonl-path> --exit-code 0
```

日志不应伪造；以真实 `go test -json` 输出及退出码为输入。Windows SDK 的 GOROOT/PATH 应仅在当前进程设置，不改变系统安装。

## 测试与安全边界

故障用例验证在绑定缺失、补查失败、nil/wrong-ID、空 host、无效 port/protocol/host 时，token cache 和 HTTP factory 都不被调用。有效绑定仍向 factory 传原等价 URL；没有绑定继续保留原默认路由；shadow 使用母账号绑定。

fake upstream 测试仅检查 service→factory→本地 httptest.Server，**不声称已经测试 Gin ingress、真实代理 CONNECT、生产 OAuth refresh、全链路禁止直连、TLS/JA3 或供应商风险评分**。

本批没有运行全仓库测试、race detector、完整前端构建、生产流量或 DB migration。身份特征测试仅证明现有 exec override 和两层开关的行为；没有将该配置自动应用到生产。

## 证据文件（本机）

全部测试/构建日志位于 `D:\Temp\sub2api-build\logs`，没有包含真实账号凭据。以下哈希在交付时计算：

- `gap-red-proxyurl.jsonl`：`bff23bd6f1b73510450e1efc8cc9aae409d44d9bb8c04d2b8ced48ff543f114c`
- `gap-red-service.jsonl`：`2f24c67e5d1a5844881e69258d501cefe386bd3f35a0200ea9795f2926e20f71`
- `gap-green.jsonl`：`5cde4b76bacddc1ca7690c9466732afcdb0a1edb911b49f063a01859e32a2633`
- `gap-proxyutil.jsonl`：`31f26888a476e0d5ac790c00b83f37da8881dace19584978c9accaf31dec72a7`
- `gap-linux-build.log`：`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`

对应 browse-dev 任务：`task_RoF0A0IUVEyeMP_laTbW`。

关键 durable jobs：parser 红测 `job_MmpUIAdEzbBLeUHlDHlA`；quota 红测 `job_FCOB9-BGXRIjc7m_6njk`；定向绿测 `job_Cttp7TFdQgn0pd37YHmu`；proxyutil 回归 `job_1fFKkzcAQpZW5oDG5SNT`；Linux 编译 `job_f1tIftWuIRpG2cvNRcOe`。

## 评审结论

最小 patch 只增加 quota 路由前置校验和共享 parser 静态脱敏错误。已有 UA resolver、convergence、WS 池、continuation 策略原样保留。生产范围不扩大。G09 全端点接入、G04 真入口 header 合同、完整 reference drift CI 等仍是后续批次，不作为本次“已完成”宣传。
