# 0.2.0-r2 执行计划

基线：`8b03a9adee756e053091ea0163565ca47af06099`（0.2.0-r1）；对照：main `38a69cbb529eb8fc1b820fa00d7cc439306112f4`（0.1.0-r6）。本轮在现有 `feat/0.2.0-policy-profiles` 开发，不合并 main、不发布正式 Release、不操作生产路由器。

## 1. 取证与基线（已完成）
1.1 固定上述两个 SHA，通过 Actions 源码归档获取完整树。下载与工具入口失败分别记录在 `docs/failures/0.2.0-r2/`，每次独立提交。
1.2 比较 `internal/netpref/{traffic,firewall,platform,runtime}.go`、RPC、LuCI、打包脚本。重要事实：r6 与 r1 的 traffic/firewall/platform 实现相同；runtime 的变动是校验结果增加库计数。不能在没有实机诊断的情况下宣称已确认一个 r1 独有的计数器算法错误。
1.3 `go test -race -count=1 ./...` 已通过。容器无常规 GitHub 网络与交互执行会话；使用同步命令，GitHub 连接器写入。真实 netns/SDK 验证由 Actions 执行。

## 2. 流量全链修复（负责人：本次执行者；待实现）
2.1 在 `internal/netpref/traffic_test.go` 及新增运行时回归测试中复现：空计数器被报告可用、Apply/设备发现重建清空会话、DNS 健康暂停导致观察不更新。保留红测证据，不把预期复现当作通过。
2.2 修改 `traffic.go`：明确区分关闭、首样本、有效、缺失计数器/错误；每个协议族独立有效性；重建仅丢弃速率基线、保留已累计值；公开采样错误与缺失项。
2.3 修改 `runtime.go`：仅 DNS 模式变化时刷新自有集合而非重建所有计数器；真实拓扑变化时重新建立基线；DNS fail-open 不应伪装为监控关闭。所有 nft 操作仍经过已有所有权/健康安全边界，不改共享服务。
2.4 扩展 `tests/netns.py`：使用真实 IPv4/IPv6 转发流量，两次采样验证四个方向速率、累计、占比、模式切换保留累计。通过 unix control/CLI RPC 检查同一数据，再交给 LuCI 测试渲染。

## 3. 用户模型与 LuCI（负责人：本次执行者；待实现）
3.1 新增纯前端库模型 `files/www/luci-static/resources/netpreference/library.js`：自动生成命名 UCI ID；名称和显示与 ID 分离；只在用户新增/编辑输入时把普通域名规范为根域+子域匹配。未编辑的旧精确规则保持原义，避免静默扩大旧 DNS 阻断范围。
3.2 重做 `view/netpreference/advanced.js`：域名集仅名称+列表；模式集内直接增删/排序无名规则；输入域名或选择域名集、四种模式及可选 A/B；参数留空继承设备，显式 0 保留。高级 DNS action 留在折叠区域，不删旧配置和未知字段。
3.3 修改 `overview.js`：设备选择模式集只显示名称，保留设备全局策略；流量每族独立展示，状态/错误互不遮蔽；页面不显示内部 ID。
3.4 为持久化前校验新增只读 `check_config` RPC：接收有界 UCI 文本，调用既有 `ParseUCI`，绝不执行网络或配置修改。LuCI 对整个待保存配置先做后端校验，再提交且仅提交 netpreference；保留原 validate 行为。
3.5 新增模型/DOM/浏览器测试：中文名称、宽松域名、旧精确语义、重复/空输入、引用删除、A/B 继承与 0、后端拒绝时不 commit、刷新后规则稳定、旧 DNS action 不丢失。

## 4. 集成和构建（待执行）
4.1 本地运行 gofmt、Go race/vet、现有 fuzz、包检查、工作流检查、Node 合同测试、Chromium 页面交互测试。失败逐项独立记录并修复。
4.2 `PKG_RELEASE` 递增至 2，维持 `PKG_VERSION=0.2.0`；通过原生 Git tree/commit 在分支原子提交，保留其它提交，不 force push。
4.3 保持 feature push/PR 完整验证，SDK 继续使用固定官方 ImmortalWrt 24.10.4 x86_64 归档及校验和；仅 main 可发布正式 Release。移除本轮临时源码快照 workflow。
4.4 验证 Actions 的真实 netns、官方 SDK、官方目标用户态 opkg 安装/r6 升级/恢复结果。每次失败按阶段独立文档和提交；修复后以对应 source SHA 的成功任务为准。

## 5. 交付（待执行）
5.1 取回实际 SDK IPK，校验 SHA256、包内版本/脚本/权限、配置保留行为；提供 IPK、校验和、源码与验证记录。
5.2 更新本计划实际完成状态和 `VERIFICATION-0.2.0-r2.md`，列出命令、任务链接、失败及恢复、未验证项目。
5.3 明确测试边界：隔离内核/官方用户态验证不等于已登录用户的真实路由器验收；不把流量软件路径观察声称为 flow offload 全量统计，不将 DNS 时序偏好声称为强制 IPv6。
