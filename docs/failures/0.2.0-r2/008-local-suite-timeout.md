# 008 — 本地整套检查达到容器执行时限

- 命令：`bash scripts/check.sh > ../evidence/r2-full-local.txt 2>&1`。
- 实际结果：container.exec 报 `Command failed because it timed out`（请求时限 120 秒）；该次整套任务不能记为通过。
- 已留日志：Go race/覆盖率通过，internal/netpref 覆盖率 80.9%；DNS fuzz 3 秒通过；UCI fuzz 3 秒通过；域名匹配 fuzz 已运行并输出至 elapsed=9s，没有获得结束结果；打包尚未开始。
- 原因边界：这是执行环境时限中断，尚无断言失败或产品错误证据。不能将被中断的 fuzz 记为通过，也不能仅据此断言匹配器失败。
- 恢复：按阶段运行剩余域名 fuzz 和打包/工作流/UI 检查并保留日志；完整脚本还必须在 GitHub Actions 的正常执行环境通过。没有修改生产路由器。
