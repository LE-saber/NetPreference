# 003 — workflow 文件名形式的读取入口被连接器拒绝

- 操作：GitHub.fetch 读取 `/actions/workflows/r2-source-snapshot.yml/runs?per_page=1`。
- 实际结果：`INVALID_ARGUMENT / HTTP 400 / URL is not an allowed public GitHub repository or search endpoint`。
- 影响：无法通过该 URL 观察源码快照任务，不是 Actions 执行失败。
- 恢复：使用原生 Actions workflow/run 工具或已验证的数字 ID 入口。
- 状态：独立记录后继续开发。
