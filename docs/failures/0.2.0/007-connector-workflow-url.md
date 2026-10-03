# 007 — 连接器拒绝按 workflow 文件名查询 runs

- 日期：2026-10-03；分支 `feat/0.2.0-policy-profiles`。
- 操作：GitHub Fetch GET `/actions/workflows/ci.yml/runs?branch=feat%2F0.2.0-policy-profiles&per_page=1`。
- 实际结果：工具端 HTTP 400 / INVALID_ARGUMENT，`not an allowed public GitHub repository or search endpoint`。
- 原因：连接器允许的 GET URL 范围不包含该文件名形式的 workflow 子路径，并非 workflow 执行失败。
- 恢复：使用 `/actions/runs?branch=feat%2F0.2.0-policy-profiles&per_page=1`，成功找到实际运行 `37087556392`；再通过 `/actions/runs/37087556392/jobs` 读取步骤。
- 状态：已恢复。不得把工具查询失败误报为 CI 构建失败；构建完成状态以实际 jobs / artifacts 为准。
