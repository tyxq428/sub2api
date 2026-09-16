# 第二批实现与验收

基线：第一批 `42798052390086a1b012f3eed05f2ab5513933b9`，官方 v0.2.5 基线不变。分支 `feature/codex-gap-v1`；独立工作区 `E:\Apps\sub2api-worktrees\codex-gap-v1`。具体交付commit可执行 `git log -1 --format=%H -- docs/codex-gap/verification-v2.md` 查询。

## 结果

- 新增13个定向主测试，含61个子测试；13/13真实运行且通过。
- 在未修复源码上，10个主测试、44个子测试实际失败；并有3个正常行为主测试通过，表明不是编译失败冒充红测。
- 首次测试代码曾因fake transport函数签名写错而编译失败，修正测试参数后重新得到上述真实红测；该编译失败不作缺陷证据。
- 首批＋第二批门禁：31/31必跑测试有run与pass，无skip/缺失；完整该组为46主测试＋130子测试。
- 扩展既有回归：10个选定测试文件、313个主测试＋130个子测试全部通过，不是全仓库测试。
- 广回归首次发现1个旧测试fixture未设置Proxy.ID（声明ProxyID=9，对象ID默认0）。补齐ID=9后重跑全部313个；缓存隔离原断言保持不变，没有关闭测试或降低校验。
- 门禁脚本的空日志、skip、无run只有pass、Go非零退出、package失败拒绝测试通过。
- Linux amd64后端交叉编译通过：CGO_ENABLED=0、不含frontend embed、未运行产物，也未构建或部署Docker镜像。
- main、VPS、数据库、账号、代理设置、时区、UA与TLS/WS协议未改；只有WS连接复用约束加入代理路由身份。

两份最终通过日志按 `(Package, Test)` 去重合计：**359 主测试＋260 子测试**。新增13项已经包含在门禁组中，不重复计数。

## 工具链和证据

沿用独立Go1.27.1 SDK：`D:\Temp\sub2api-build\go1.27.1\go`；进程级GOROOT/GOCACHE/GOMODCACHE、GOTOOLCHAIN=local、GOWORK=off、CGO_ENABLED=0、-mod=readonly、-count=1。没有修改系统Go或PATH，没有改go.mod/go.sum。所有账号/token均为合成测试数据，仅本地httptest服务和模拟transport，无生产OpenAI调用。

测试日志目录：`D:\Temp\sub2api-build\logs`。

- `gap-v2-test-compile-initial.jsonl` SHA256：`aa20c167ac7872ed396e197afdb1912c751341bcc61fee9acc96fa7bcf6d1a14`
- `gap-v2-red.jsonl` SHA256：`bf95968a56235452cf9711c7e25d18c1bf2660d5b955236228712e4dfb311fb8`
- `gap-v2-green.jsonl` SHA256：`00c5444195ad30e72f7cba9748cb13f09118d1af78ec8811fd83eea7d9bcc2fe`
- `gap-v2-regression.jsonl` SHA256：`285947b7b4abd0592b2287d6f9b8efed93df38ffee956d0a4c19a8526d5ff876`
- `gap-v2-combined-final.jsonl` SHA256：`6b38040efba373b40ffc4eb80dc90d5c3a795a5b61f6b6afbfb77b10e6c17ac6`
- `gap-v2-regression-final.jsonl` SHA256：`46e443b032b3cbd12b8915088a2018da9ce770fb1209c3c76387e161f7ebe632`
- `gap-v2-regression-selection.json` SHA256：`ea771e8fd53f88cd8f6823edb6a68163eb65a5e6f3eba0039ee03d36216925e2`
- `gap-v2-linux-build.log` SHA256：`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`

Durable task：`task_9sXAD52MNYfI8hEZoWMB`。
- 真实红测：`job_TKu4NPeQ2av2mWUGdSJ4`。
- 定向绿测：`job_SY-K3qLCYujoKtBFwuMg`。
- 最终门禁＋313既有回归：`job_l9j9TbrX_EqGA4lIasnC`。
- Linux后端交叉编译：`job_qmK66RvQNifPew5afzKH`。

## 可重放

```text
python backend/scripts/check-codex-gap-tests.py --self-test
python backend/scripts/check-codex-gap-tests.py --go <compatible-go-executable> --log <jsonl-path>
```

313项既有回归名称和对应文件保存在 `gap-v2-regression-selection.json`，执行go test时以这些名称的精确正则筛选，并逐项核对run/pass。全量race、Gin/Nginx端到端、生产代理CONNECT/TLS、完整Docker/前端构建尚未实施，不能由本批结果推定通过。更没有上游风控评分或封号概率的测量。

## 回滚与后续

本批为一个独立本地commit，不push、不合并main、不部署；必要时revert该commit。下一步按覆盖台账补专用WS passthrough、Agent Identity注册及独立辅助通道，或进行部署前隔离实例验证，不应把“公共入口通过”写成全端点通过。
