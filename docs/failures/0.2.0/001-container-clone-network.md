# 001 — 开发容器无法直接克隆仓库

- 日期：2026-10-03。
- 基线：`main` / `38a69cbb529eb8fc1b820fa00d7cc439306112f4`。
- 分支：`feat/0.2.0-policy-profiles`。
- 阶段：取得源码，尚未修改产品代码。
- 命令：`git clone --branch main https://github.com/LE-saber/NetPreference.git /mnt/data/NetPreference`。
- 实际结果：exit 128，`Could not resolve host: github.com`。
- 原因：当前执行容器的直接网络/DNS访问不可用；这不是仓库权限或产品故障。GitHub 连接器已成功读取最新 main 并建立开发分支。
- 影响：不能依赖容器内 git clone、git push 或联网下载 SDK/Go 依赖。
- 恢复路径：通过已授权 GitHub 连接器读取/提交源码；容器用于可离线执行的实现和测试；完整联网测试与官方 SDK 构建由 GitHub Actions 实际执行并检查结果。
- 安全状态：未改 main，未访问或修改用户路由器，未改 HomeProxy/SmartDNS/NAT66。
- 状态：已采用替代执行路径，继续开发；不把未执行的构建记为成功。
