# 010 — 延长差分 fuzz 发现查询名规范化非幂等

- 日期：2026-10-03；分支 `feat/0.2.0-policy-profiles`。
- 阶段：首轮官方构建通过后，额外进行 30 秒双 worker DomainSet 差分 fuzz。
- 命令：`go test ./internal/netpref -run '^$' -fuzz '^FuzzDomainSetMatchesLinearReference$' -fuzztime=30s -parallel=2`。
- 实际结果：约 17.64 秒、19 万次执行后失败。保存反例 `1b80e8a98011dc15`：模式 `0`、输入查询名 `0..`；集合索引得到精确命中，线性匹配不命中。
- 根因：`normalizeQueryName` 每次剥除一个结尾根域点；selector 与集合 matcher 在不同层重复调用，错误把非法的双尾点逐层变为合法域名。规范化不是幂等操作，因此直接域名与集合路径不一致。真实 DNS wire 解析已转义标签内的点，但内部匹配 API 仍必须一致处理这种边界。
- 修复：只对单一结尾根域点做规范化，连续尾点保留为不匹配的原有结构；ASCII 大小写转换保持。增加幂等性、直接/集合一致性、双尾点不扩大匹配的回归测试并保留 fuzz 反例，再重跑延长 fuzz、完整本地 suite、分支/PR 与官方 SDK。
- 交付约束：此前成功的 IPK 仅保留为历史构建证据，不作为最终交付包；修复后重新构建并核验新的官方产物。
- 影响：开发验证中断，无 main 或实体路由器修改。状态：已定位，执行修复及复验。
