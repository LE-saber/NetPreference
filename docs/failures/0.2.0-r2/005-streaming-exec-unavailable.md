# 005 — 基线测试交互式执行方式不可用

- 操作：通过 container.exec 的 session_name 启动 `go test -race -count=1 ./...`。
- 实际结果：`StreamingExecNotEnabledContainerError`，未获得测试结果。
- 原因：本容器不支持交互式持续会话。
- 影响：不能把该次调用当作已执行或已通过的测试；不影响源代码。
- 恢复：改为普通同步命令，分段运行测试并保留日志，不再使用 session_name。
