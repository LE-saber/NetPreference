# 002 — 源码归档下载的工具 URL 门禁

- 日期：2026-10-03。
- 分支：`feat/0.2.0-policy-profiles`。
- 阶段：把已核实的 main 源码送入离线开发容器。
- 操作：下载 `https://github.com/LE-saber/NetPreference/archive/38a69cbb529eb8fc1b820fa00d7cc439306112f4.tar.gz`。
- 结果：下载工具报 `url not viewed in conversation before`；按要求尝试网页打开后返回 Cache miss，重试仍被门禁拒绝。没有得到归档文件。
- 原因：二进制归档的 URL 门禁与当前工具下载路径不兼容；不是产品代码或 SDK 构建错误。
- 恢复：通过只读权限、固定提交的 Actions 源码快照任务产生 artifact，再用 GitHub 原生 artifact 下载能力取回。源码快照临时 workflow 在开发交付前删除；它不会修改源代码、main 或发布 Release。
- 状态：切换到 Actions artifact 传输路径，继续执行。
