# R1 · B3b 专用 WS 与辅助请求的代理绑定

复用既有resolver，专用WS透传在header和Dial前校验；图片下载使用凭据归属账号的代理且不带Authorization/Cookie；alpha search与live sideband接入同一辅助路由helper。没有改变帧协议、URL安全检查、隐私设置或签名。

红测 `job_FgRMKGF3dfSebjyZlIEM`：三个新增主测试失败，独立WS三个故障子场景失败；上游是本地fake Dial/HTTP。有效WS路由、图片正常下载回归保持。

一次旧fixture漏填Proxy.ID，导致首轮66项中65项通过。仅补其 `ID: proxyID`，原下载头/重定向/输出断言原样保留，并完整重跑。不能把该次失败记录删除或算绿测。

最终联合验收 job `job_kuwN9qKVYX1fjsU4KpRd`：72个主测试＋40个子测试通过，72/72要求项实际run/pass；本机日志 `b3-green-auxiliary-redirect-final.jsonl`，SHA256 `543712f96fb5d29bccf58f3915666f33d06c63522565a6307b73820dbf152076`。该联合记录不可与B3b/B3c分项重复相加。

测试覆盖独立实际loopback WS入口与图片fake dispatch；alpha/live执行既有回归，但不将既有回归冒充新增完整故障矩阵。Agent Identity签名/注册、插件内部网络、全局撤销仍未验证。本批不改变其他平台的默认路由，不进行生产或真实模型调用。
