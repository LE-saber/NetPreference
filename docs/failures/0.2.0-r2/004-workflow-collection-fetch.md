# 004 — workflow 集合读取入口被连接器拒绝

- 操作：GitHub.fetch 读取 `/actions/workflows`。
- 实际结果：`INVALID_ARGUMENT / HTTP 400 / URL is not an allowed public GitHub repository or search endpoint`。
- 影响：不能从该集合发现 workflow ID；源码快照实际已成功，不受此错误影响。
- 恢复结果：通过已支持的 `/actions/runs` 发现快照 run `37127708044`，原生 artifact 工具成功下载 `11275673957`。容器已解压 r1 与 r6 源码，开发恢复。
- 后续：仅使用已验证的 runs 集合及原生 job/artifact 工具。
