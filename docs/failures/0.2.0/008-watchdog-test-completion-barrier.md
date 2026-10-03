# 008 — PR 内核测试把删表误当作恢复已完成

- 日期：2026-10-03；分支 `feat/0.2.0-policy-profiles`。
- 运行：https://github.com/LE-saber/NetPreference/actions/runs/37087764739 ，verify job `111101482509`。
- 输入：PR head `a14c9c0c9202f624f936c64021c2ea6e3768ce02`，合并测试提交 `31c625accba51f60178ca645a1cde07ddb7ee1c7`。
- 命令：`sudo python3 tests/netns.py`。
- 实际结果：核心测试、Profile 双栈 UDP/TCP 时序、旧规则、无效引用回滚、显式 Restore、流量及 NAT66 均已通过；最后 SIGKILL 场景复用原 UDP 源端口 42054 时收到 `ConnectionRefusedError`。SDK 因 verify 失败被正确跳过，未发布 Release。

## 原因

检查 `Firewall.Remove` 和测试可知，恢复顺序是先删除本插件 nft 表，再逐个本地地址清理属于插件的 DNS conntrack。它们是两个不同的内核接口，删表完成不等于后续 conntrack 清理已完成。原测试仅等待 nft 表消失，随即复用旧 NAT 五元组，可能在清理仍执行时查询已被 SIGKILL 的 1053 端口。失败后的 finally 又终止 guard，日志中的 `conntrack: context canceled` 是该终止阶段的结果。此前同代码的分支内核实验成功，PR 的不同调度暴露了这个错误完成条件。

## 修复与验收

保留原 15 秒总恢复上限及原源端口，不盲目加 sleep，不重试 DNS 直到碰巧成功，也不删除 SIGKILL 断言。测试必须同时观察：本插件表消失、该固定五元组的插件 DNAT conntrack 消失；随后只做一次原端口 DNS 查询并断言返回原 DNS 答案。若实际 guard 未完成清理，测试仍然超时失败。对修正后的内核实验重复执行，保留实际日志。

- 产品的防火墙所有权、恢复顺序和边界没有因测试修改而放宽。
- main / 实体路由器未修改。
- 状态：根因已定位，正在修正测试完成屏障；修复后的实际 CI 结果另记于验证报告。
