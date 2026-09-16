# R1 · B3c 凭据请求重定向边界

原始OAuth models独立客户端与privacy工厂都能跟随上游重定向到第二个本地目标。红测 `job_4tqCuTaQskdqP0slXBvI` 两个主测试失败，证明问题在真实client而非mock-helper。

增加按Options启用的DisableRedirects，缓存key包含该选项，不原地修改已有缓存client。只在models OAuth独立客户端和OpenAI privacy工厂开启，保留其他provider默认行为。响应3xx继续由各调用方既有错误处理识别。图片匿名CDN下载仍保留原public-host逐跳验证，没有全局禁止所有重定向。

新增验证protected/default缓存隔离、默认client仍跟随；其余CONNECT407/502/302不能绕过显式代理、NO_PROXY不能覆盖显式绑定、SOCKS5H传hostname的测试原版已通过，属于特征验证，不宣称是本批修复。

最终联合验收 job `job_kuwN9qKVYX1fjsU4KpRd`：72个主测试＋40个子测试通过，72/72要求项实际run/pass；本机日志 `b3-green-auxiliary-redirect-final.jsonl`，SHA256 `543712f96fb5d29bccf58f3915666f33d06c63522565a6307b73820dbf152076`。该联合记录不可与B3b/B3c分项重复相加。

未覆盖OAuth token repository、第三方插件和全部跨域链；本记录不是OpenAI全站redirect审计通过。测试只有loopback目标与合成凭据，未降低TLS校验、未修改浏览器模拟配置、未触碰生产。
