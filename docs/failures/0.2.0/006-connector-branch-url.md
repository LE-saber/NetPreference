# 006 — 连接器拒绝编码后的 branch 路径

- 日期：2026-10-03；分支 `feat/0.2.0-policy-profiles`。
- 操作：通过 GitHub Fetch GET `/branches/feat%2F0.2.0-policy-profiles` 读取开发分支。
- 实际结果：工具端 HTTP 400 / INVALID_ARGUMENT，`GitHub Fetch URL contains an invalid repository path`。
- 原因：该连接器路径校验拒绝此编码形式；未执行任何仓库写入，不是 GitHub 分支缺失或产品故障。
- 恢复：改用受支持的 Git data URL `/git/ref/heads/feat/0.2.0-policy-profiles`，成功取得 `29fb90feacf73da962073afbc43e2351ce592989`，并从对应 commit 读取 tree。
- 状态：已恢复；checksum 校验源码传输成功，随后正常提交两份 CI workflow。main 和路由器均未改变。
