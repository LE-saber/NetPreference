# 013 — 测试 DNS fixture 的 UDP 随机端口与 TCP 冲突

- 日期：2026-10-03；PR run `37089182102`，verify job `111106776826`，head `a8c9d5fc0e5780bd567c6e06ad6eb5c3a3007209`。
- 命令：`bash scripts/check.sh` 中的 Go race 单元/协议测试。
- 失败位置：`TestOldInflightProbeCannotPolluteNewProfile` 创建 fixture 时，`listen tcp4 127.0.0.1:39078: bind: address already in use`；尚未执行该用例的缓存隔离断言。SDK 和 Release 正确跳过。
- 根因：旧 fixture 先让 UDP 自动选择端口，再假定同号 TCP 端口也可用。UDP 分配成功并不预留 TCP 端口，TCP 可能已经被其他 socket/TIME_WAIT 占用；第二次 bind 失败时 fixture 还没有注册 cleanup，旧代码同时漏关先建立的 UDP socket。这与 Profile 的运行时缓存逻辑不同，但导致完整测试不稳定，不能只重跑直到碰巧成功。
- 修复方向：测试 fixture 先保留 TCP 自动选择的端口，再绑定同号 UDP；只对明确的地址占用错误有限次重新分配，任何其他错误直接失败；每个失败的候选 socket 必须关闭。对使用产品 ListenDNS(port=0) 的测试同样只在随机端口分配阶段处理明确冲突，DNS 查询和产品断言绝不重试或放宽。添加确定性注入冲突及失败资源释放测试。
- 验收：重复执行新分配器回归与旧在途探测隔离用例，再执行分支和 PR 完整 verify、官方 SDK/rootfs。产品固定端口行为及配置不变，不重试或掩盖实际 DNS 错误。
- 已有证据：同产品源码的上一 PR run `37089063061` 已通过完整 verify 与官方 SDK/rootfs；本次暴露 fixture 环境假设，最终仍需以修复后的最新检查为准。
- 影响：仅隔离 CI 测试失败，无 main、正式 Release 或实体路由器修改。
