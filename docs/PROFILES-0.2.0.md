# 0.2.0 使用说明：设备默认 + 命名高级模式

## 一、最重要的关系

每台设备始终保留自己的普通双栈 / IPv6 优先 / IPv4 优先 / 自定义模式、A、B、主动探测及上游设置。选择 Profile 只是叠加该模式下的**域名覆盖**，不是把设备默认配置替换成公共配置。

```text
设备自己的默认策略
   └── 可选 Profile（例如“工作模式”）
        ├── 域名偏好规则 → 直接域名，或引用一个命名域名集
        └── 可选 DNS 动作 → 直接域名，或引用一个命名域名集
```

没有选择 Profile、没有命中该 Profile 的域名规则：继续使用设备默认。把设备的 Profile 改成“不使用高级模式”即可退出高级偏好，不需要重新填写默认 A/B。原有不属于任何 Profile 的 DNS action 仍然生效。

一个 Profile 可以给多台设备共用；未填写的字段分别继承各台设备的默认，因此同一个模式在不同设备上可以得到不同的 A/B。DomainSet 是可跨 Profile 共用的命名集合；修改共享集合会影响所有引用它的规则。当前不提供递归集合、Profile 继承链或自动复制模式。

## 二、LuCI 操作

入口：**服务 → NetPreference → 设备与状态 / 高级模式与域名集**。

普通用户只使用设备页即可。原有状态、设备默认、流量监控、Apply / Restore 都保留；设备编辑项增加可选的“高级模式 / Profile”。高级页面把复杂配置单独分为四部分：

1. **命名模式 / Profiles**：新建稳定 ID，例如 `work`，填写显示名称“工作模式”。ID 使用 1–64 位字母、数字或下划线；名称可用中文。引用的是 ID，修改显示名称不会断开引用。
2. **命名域名集 / DomainSets**：新建 `media`，名称“影音站点”，一行一个域名或通配模式。
3. **域名偏好覆盖**：选择 Profile，直接填写域名或选择集合（二选一），选择偏好，按需在编辑窗口填写 A/B 和主动探测。留空不是零，而是继承设备。
4. **DNS 动作**：原有域名 DNS 规则移到这里；没有丢失旧规则。可选 Profile 限定；留空则维持原来的常驻规则。

高级页 **Save** 保存模式库并验证，不切换当前运行策略；回到设备页选择模式后 **Save & Apply**。高级页 **Save & Apply** 则同时应用当前设备已经选择的模式。修改已被设备使用的共享 Profile 时，“应用”会更新这些设备。

保存只提交 `netpreference` 自身的 UCI 包，不调用全局 `uci.apply()`，不会顺带应用其他网络页面未提交的修改。前端检查悬空引用、选择器冲突和继承时限；后端仍独立验证。无效配置不能替换正在运行的有效策略。若后端报告保存文件中的其他错误，应按提示修复；不要把错误提示当作已应用成功。

删除 Profile 或 DomainSet 前，先移除设备/规则对它的引用；直接删除仍被引用的条目会被拒绝。模式库可在尚未分配给设备时先创建好。

## 三、域名与优先级

- `example.com`：仅该域名。
- `*.example.com`：**包含 example.com 本身和所有下级域名**，保持 0.1.0 的既有语义；不会匹配 `notexample.com`。
- `*`：全部域名；用于模式的默认覆盖时要有意选择。
- 配置大小写与单个结尾根域点会规范化；国际化域名使用 punycode。禁止空通配后缀 `*.`、空标签、递归集合。

**偏好规则**只在设备选择的 Profile 中竞争，按命中域名的具体度排序；精确域名胜过同根通配，集合使用本次命中的最具体成员。完全同分按 UCI / LuCI 列表顺序，只选一条。没有多条规则逐级叠加：胜出规则未设置的字段直接回到设备默认。

**DNS action**与偏好分开匹配：先设备范围，再域名具体度，完全同分时 Profile 限定的 action 优先，最后按列表顺序。设备整机 block 最高；时序偏好不能解除 NXDOMAIN、NODATA 等内容策略。显式创建更高优先级 DNS action 是另外一回事，不是偏好规则的副作用。

所有“全局”规则仍只影响已经加入本插件的设备。未选择设备保持原 DNS 链路，不因创建 Profile / DomainSet 被自动接管。

## 四、A/B 与继承

A：从非首选地址查询进入策略处理起，等待首选地址证据的有限窗口；不是每次查完上游后再额外睡眠 A。

B：只有确认首选地址记录存在，才给非首选地址答复增加延迟，并受 A+B 总窗口限制。没有首选地址记录时保留有效的另一族答复，不无限等待、不默认删除 IPv4。

| 高级规则输入 | 结果 |
|---|---|
| 模式“继承设备默认” | 保持该设备的模式，仍可单独覆盖 A/B/探测 |
| A 或 B 留空 | 继承设备对应数值 |
| A 或 B 填 0 | 明确设为零，不会被默认值替代 |
| 主动探测“继承设备” | 使用设备开关 |
| 主动探测“关闭” | 明确关闭，即使设备开启 |
| 自定义模式、首选协议留空 | 继承设备有效首选族；IPv4/IPv6 优先设备按其实际族继承 |
| 普通双栈 | 不人为增加 A/AAAA 偏好延迟 |

继承后的每个组合均要求 `0≤A≤750 ms`、`0≤B≤500 ms`、`A+B≤1000 ms`。后端检查所有已引用的设备以及尚未分配模式的默认组合。A/B 不改变原有普通上游超时；本地静态重写、拒绝和否定答复立即返回，不再附加时序。

主动探测使用首选查询类型自己的 DNS action 和上游，不能绕过该类型的阻断，也不会把只适用于 A 的上游自动拿来查询 AAAA。切换 Profile 后证据缓存按配置隔离，旧的在途探测不能污染新模式。

## 五、一个完整例子

目标：工作电脑默认 IPv4 优先，A=75、B=40；影音集合 IPv6 优先，A=250、B=150；其中一个会议域名仍 IPv4 优先，A=40、B=20。

下面是配置结构示例，**不是要求覆盖你现有的整个 `/etc/config/netpreference` 文件**。真实 MAC、接口和原有 global / device 项应保留，建议通过 LuCI 编辑。

```uci
config device 'workstation'
 option mac '02:00:00:00:00:01'
 option mode 'ipv4'
 option wait_ms '75'
 option delay_ms '40'
 option profile 'work'

config profile 'work'
 option name '工作模式'

config domain_set 'media'
 option name '影音站点'
 list domain '*.example.com'
 list domain 'video.example.net'

config policy 'media_v6'
 option profile 'work'
 option domain_set 'media'
 option mode 'ipv6'
 option wait_ms '250'
 option delay_ms '150'

config policy 'meeting_v4'
 option profile 'work'
 option domain 'meeting.example.com'
 option mode 'ipv4'
 option wait_ms '40'
 option delay_ms '20'
```

`video.example.com` 命中影音集合；`meeting.example.com` 的精确规则更具体；`unlisted.example.org` 没有命中，仍使用电脑的 IPv4 默认 75/40。另一台默认双栈设备选择同一工作模式后，未命中的域名仍双栈。

切换到另一个已保存的 Profile，只需改设备的 Profile 下拉项并应用；不需要逐条改域名规则。

## 六、从 0.1.0-r6 升级

新包版本为 `0.2.0-r1`。使用官方 SDK 的 x86_64 构建包，正常执行 `opkg install` 升级即可，不需要先卸载，不需要重建配置。`/etc/config/netpreference` 继续声明为 conffile；旧 device / rule 原字段和原动作语义保留。旧配置没有 Profile 选项，升级后自然继续使用原设备默认和原 DNS action。

```sh
cd /tmp
sha256sum -c SHA256SUMS
opkg install ./luci-app-netpreference_0.2.0-r1_x86_64.ipk
netpreference version
```

不要加 `--force-maintainer` 去替换已修改的配置，也不要使用 `--force-depends` 掩盖不匹配的系统依赖。安装会重启本插件及刷新 rpcd；DNS 客户端可能需要重试正在进行的请求，不能承诺升级零中断。保留现有配置后刷新 LuCI 页面。

恢复仍用 `netpreference restore` 或页面 Restore，仅删除本插件所有权范围内的拦截，不回写旧版整份系统配置。HomeProxy、SmartDNS、NAT66、dnsmasq、nlbwmon 不被改写或卸载。流量统计继续采用 r6 的插件自身 nft 会话计数，不恢复 nlbwmon 累计依赖。

## 七、构建与验证边界

所有 feature 分支的相关代码提交和 PR 都执行完整验证，再调用官方 ImmortalWrt 24.10.4 SDK。开发分支只提供 Actions artifacts；正式 Release 仅由通过验证与 SDK 的 main push 创建，不覆盖已有 Release。

测试入口：`bash scripts/check.sh`；隔离内核实验 `sudo python3 tests/netns.py`；真实 opkg 升级实验先运行 `bash scripts/build_legacy.sh`，再运行 `sudo bash tests/rootfs_install.sh`。SDK 可复用 workflow 对实际 SDK 产物执行包内容校验和 rootfs 升级实验。

本地 UDP/TCP 测试证明 DNS 时序和规则行为；Linux namespace 实验证明相应 nft/conntrack/恢复行为；官方 rootfs 实验证明 opkg 格式、版本升级及配置保留。它们都不等同于你的实体路由器已完成 LuCI 浏览器、HomeProxy/SmartDNS 现场验收。

DNS 偏好不是强制连接协议：应用缓存、Happy Eyeballs、DoH/DoT、VPN DNS、HTTPS/SVCB 和实际 IPv6 可达性仍影响最终连接。软件/硬件 flow offload 也可能导致流量观察低估，插件不会自动关闭 offload。
