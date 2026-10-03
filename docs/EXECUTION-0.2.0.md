# NetPreference 0.2.0 主执行计划与完成记录

日期：2026-10-03。基线 `main@38a69cbb529eb8fc1b820fa00d7cc439306112f4` / `0.1.0-r6`；分支 `feat/0.2.0-policy-profiles`；目标 `0.2.0-r1`。本轮助手直接负责高难度核心实现和集成；没有虚构代理或独立审查。

授权仅限目标仓库分支、测试和构建，不合并 main、不发布开发 Release、不访问生产路由器。最终运行状态与产物精确来源固定在 [PR #2](https://github.com/LE-saber/NetPreference/pull/2) 验收清单及对应 artifact 的 build-info 中。实现/测试细节见 [验证报告](VERIFICATION-0.2.0.md)，操作见 [使用说明](PROFILES-0.2.0.md)。

## 1. 取得基线与环境 — 已完成

1.1 通过 GitHub 连接器核实 main SHA、版本和写权限，从准确 SHA 建分支。读取 `internal/netpref/{config,policy,runtime,firewall}.go`、LuCI、SDK Makefile 和两份 workflow，提取设备策略、恢复所有权、nft 计数器和升级契约。
1.2 容器直接网络不可用，采用只读 Actions 源码快照取得完整文件并核验 SHA256。实施后使用校验和保护的临时传输任务写回分支。临时 workflow 和 `.development/` 已从交付树移除。
1.3 按 `scripts/check.sh` 建立 r6 基线；冷构建工具超时单独记录后分步完成。原 Go 核心覆盖率 76.9%，原功能 tests/fuzz/package/LuCI 契约通过。
1.4 保留本地原始快照，不用不完整的本地 git 历史覆盖远端。每次导致中断的失败在 `docs/failures/0.2.0/` 独立提交，修复后重测；现有 001–011。

## 2. 数据模型与匹配核心 — 已实现并直接验证

2.1 修改 `config.go`，新增 `profiles.go`：旧 device 全字段仍是设备默认；新增可选 device.profile、命名 profile、扁平 domain_set 与归属于模式的 policy。旧 rule 保留，可选引用 Profile/DomainSet。
2.2 Profile 只组织域名覆盖与可选 DNS action，不替换设备默认；无匹配直接回落。直接域名与集合二选一；集合不递归；引用稳定 UCI section ID，名称可单独修改。
2.3 nullable A/B/probe 区分继承、显式 0、false。只取最具体单条策略，缺省字段直接继承设备，不叠加次优规则。所有实际继承组合要求 A<=750、B<=500、A+B<=1000ms；没有无限等待。
2.4 集合按实际命中成员具体度排序，精确优先，同分按 UCI 顺序；保留通配包含根域的旧语义。分离配置和查询名规范化；查询不去空白，只作 ASCII 大小写处理和单一根域点规范化。延长 fuzz 发现双尾点重复剥除后，修为幂等并保留反例。DNS wire label 转义防止标签内点/非 ASCII 伪装。
2.5 DNS action 与时序独立匹配：设备范围优先、然后域名具体度、同分时 Profile 范围和顺序；整机 block 最高，偏好不能解除阻断。本地否定和静态响应立即返回。
2.6 `policy.go` 采用深拷贝不可变配置快照，A/AAAA 各自解析动作与上游；探测不得借用另一类型的专用上游或绕过阻断；证据缓存按配置隔离，旧在途探测不污染新模式，有效 IPv4 不被偏好删除。
2.7 失败路径：非法引用、选择器冲突、继承超限在网络修改前拒绝，保留当前有效策略。未改变 `firewall.go` 所有权/恢复协议或共享 DNS/NAT66 配置。
2.8 验证入口：profiles/profile_behavior/normalization 测试、两个保留的差分 fuzz 反例，以及完整 race、socket、内核实验。

## 3. LuCI 与 r6 兼容 — 已实现，现场边界明确

3.1 overview 保留设备默认、A/B、概览、流量、Apply/Restore，增加可选 Profile 下拉项；普通设置不被高级规则表挤占。
3.2 新 advanced 页面四区：命名 Profiles、DomainSets、域名偏好覆盖、原 DNS action。稳定命名引用，留空与显式零区分，旧规则移动展示而非重建。
3.3 Save 仅提交 netpreference 并验证；Save & Apply 更新当前模式。前端检查悬空引用与时限，后端独立验证；不调用全局 uci.apply。无效保存文件仍需修正，但旧运行策略不被无效 Apply 替换。
3.4 保留 conffiles，不覆盖整份配置；真实 r6 fixture 检查旧字段/动作。SDK 新包能用正常 opkg 升级，不要求先卸载或用户重建配置。
3.5 同步二进制、Makefile、包和文档版本；包测试使用确定的当前 IPK，不误选目录残留。8 项包测试及 LuCI Node API 契约通过；真实浏览器和目标路由器 procd 联调不在已完成声明内。

## 4. 自动化集成 — 已建立并执行，最终 head 必须重测

4.1 `bash scripts/check.sh`：Go race、vet、DNS/UCI fuzz、30 秒双 worker DomainSet fuzz、8 项包测试、5 项 workflow 守卫、LuCI 契约、Python/shell 语法。010 修复后独立 30 秒 fuzz 通过 610275 次；本地 race 核心覆盖率 80.5%；其他检查分步通过。011 记录整条命令的本地工具时限，最终全套以 Actions 为准。
4.2 真实 UDP/TCP socket 验证不同域名 A/B、双向协议偏好、关闭主动探测、无 AAAA 时保留 IPv4、Profile 切换、旧在途探测和独立上游。
4.3 `sudo python3 tests/netns.py` 在隔离 runner 验证 IPv4/IPv6 nft 重定向、设备隔离、Profile 切换、无效 reload 保留运行策略、显式 Restore、同 UDP tuple conntrack 恢复、SIGKILL guard、转发计数和原 NAT66 哨兵保留。008 修复测试完成屏障后分支 verify 已通过，最终源码再跑同套。
4.4 `scripts/build_legacy.sh` 从准确 r6 SHA 构建真实旧包；`tests/rootfs_install.sh` 在官方 24.10.4 rootfs 使用真实 opkg 新装/删除/升级，逐字节核对旧配置，卸载保留用户修改的 conffile。为避免服务进入宿主，跳过在线 hooks；不能声称生产整机生命周期已验收。
4.5 任何新断言失败均先记录、再定位、修复和重测；不以删测试或放宽断言换绿色。因 010 修改了产品源码，旧成功 IPK 不再作为最终交付。

## 5. Actions 与官方 SDK — 已实现，按准确提交验证交付

5.1 所有 feature 分支相关代码 push 与 PR 执行完整验证；成功后调用官方 SDK reusable workflow。默认只读；失败记录只在合适的非 main push 分支逐项提交，PR 不写目标分支。
5.2 固定官方 ImmortalWrt 24.10.4 x86_64 SDK URL/SHA；官方 Go helper 编译静态包。检查包版本、架构、内容、二进制；记录实际 feed commit、source SHA、SHA256、源码快照和日志；SDK 产物必须通过真实 rootfs 升级。
5.3 首次分支 run 37087556392 已成功证明完整 SDK/安装链路；后续规范化修复必须重新完整构建。最终只交付包含 010 修复且实际通过 verify、SDK/rootfs 的 artifact；run 与包哈希写入 PR 验收清单，不能把排队当作通过。
5.4 Release 仅允许成功的 main push，已有 Release 不覆盖。feature/PR/手动 SDK 只产出 artifacts。本轮不合并 main，不发布正式版本。
5.5 核对远端 head、PR、测试对应 SHA；下载官方 artifact，核验 ZIP/IPK 校验和与源码，执行只读 CLI 冒烟；交付包、SHA256、源码快照、使用说明和失败记录。详细执行证据跟随同一 artifact，避免混用不同构建的哈希。
5.6 若工具读写或构建失败，保留分支和日志，逐次独立提交失败原因，再继续可逆工作。只有真实成功的结果可进入最终报告。

## 6. 后续现场验收与非目标

未访问用户实体路由器；其指定 LuCI 浏览器、procd 在线升级及 HomeProxy/SmartDNS 整机联调留给现场验收。建议首先在单台非关键设备验证保存/切换/恢复；不把已有隔离测试等同现场已完成。

不实现 NAT64、强制 IPv6、DoH/DoT 接管、连接层协议保证、递归集合或复杂 Profile 继承链。保留 r6 自有 nft 会话统计，不恢复 nlbwmon 累计依赖；不改共享 DNS、代理或 NAT66，不自动关闭 offload。
