# 006 — 流量链路新增回归测试红测

基线：0.2.0-r1 `8b03a9a`。命令：`go test ./internal/netpref -run 'TestTraffic(Runtime|Missing|Profile|Continues)' -count=1`，实际退出 1。

本次一次测试批次复现四处问题：

1. `TestTrafficRuntimeImmediateEnabled`：monitor=1 且 Apply 成功，但初始 runtime snapshot 报 enabled=false。
2. `TestTrafficMissingObjectsAreNotAvailable`：nft JSON 为合法的空 nftables 数组时，四个命名 counter 都不存在，却返回 totals_available=true 且无错误，UI 只能无限等待有效采样。
3. `TestTrafficProfileReloadPreservesCountersAndSession`：仅新增一个 Profile 再 Reload，仍产生 `delete table`，导致所有计数器和已累计会话被重建/清空。
4. `TestTrafficContinuesDuringDNSFailOpen`：DNS 健康失败时，真实 nft 数值从基线增长，但 Tick 提前 return，观察值不再更新。

这是测试驱动修复前的预期红测，不能作为通过证据。r6 对照显示这些底层代码原本也存在；尚无用户路由器现场 nft/RPC 输出，不能断言其中某一项就是该用户实机故障的唯一原因。修复将覆盖实际观察到的链路弱点并补充可见诊断，后续用运行时、控制 socket、CLI RPC 和真实 netns 流量验证。
