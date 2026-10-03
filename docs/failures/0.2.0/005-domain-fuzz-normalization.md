# 005 — 域名集差分 fuzz 发现查询名被错误去空白

- 日期：2026-10-03。
- 阶段：完整本地验证中新加入的域名集差分 fuzz。
- 命令：`go test ./internal/netpref -run '^$' -fuzz '^FuzzDomainSetMatchesLinearReference$' -fuzztime=3s -parallel=1`。
- 实际结果：第 1696 次附近发现反例：集合 `*.example.com,api,example.com.*`，查询名 `api `，索引匹配得到 `api`，线性参考匹配不命中；保存种子 `8a7e95f812a8d963`。
- 原因：配置域名规范化（可去掉用户输入两端空白）被错误复用于查询名。查询名中的空白不应被当成配置编辑空白移除，否则会扩大匹配范围。
- 修复：分离查询名规范化和配置域名规范化。查询名仅统一大小写和根域尾点，不去掉标签内空白；保留 fuzz 反例并新增精确回归测试，之后重跑 fuzz 和整套验证。
- 影响：尚未部署，main 未改变。此失败证明实际执行了新 matcher 的差分 fuzz，未以静态检查代替行为测试。
