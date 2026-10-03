# 002 — 固定 SHA 源码归档获取受阻

- 阶段：恢复容器源码获取。
- 归档：`https://codeload.github.com/LE-saber/NetPreference/tar.gz/e8792ec1ea8fa24a17217e7e80d21c970f9cc9c2`。
- 实际结果：web.open 返回 Internal Error；container.download 返回 `url not viewed in conversation before`。
- 原因：下载工具要求可识别的已访问 URL，本次 web 打开未能建立该条件。
- 影响：仅本地源码获取未完成；没有修改用户路由器或默认分支。
- 恢复：读取此前同类故障的恢复记录，改用受支持归档入口；必要时通过 GitHub 文件读取能力恢复源码。
- 状态：本次失败独立提交；继续开发。
