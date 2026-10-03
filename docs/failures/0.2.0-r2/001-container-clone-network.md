# 001 — 容器源码克隆被网络环境阻断

- 阶段：0.2.0-r2 修复准备。
- 基线：`feat/0.2.0-policy-profiles`，`8b03a9adee756e053091ea0163565ca47af06099`。
- 实际命令：`git clone https://github.com/LE-saber/NetPreference.git /mnt/data/netpreference-work/repo`。
- 实际结果：退出码 128，`Could not resolve host: github.com`。
- 原因：当前执行容器无法通过常规 git 网络访问 GitHub；GitHub 连接器已成功读取仓库及分支。
- 影响：本地基线检查暂未执行，不代表项目代码失败。
- 恢复：使用可用的源码下载/连接器能力获取已固定 SHA 的源码，在容器运行测试，通过 GitHub 连接器写入本分支。保留 main 和用户路由器不变。
- 状态：本次失败独立提交；继续开发。
