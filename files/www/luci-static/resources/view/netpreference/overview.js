'use strict';
'require view';
'require form';
'require rpc';
'require uci';
'require ui';
'require poll';
'require dom';
'require netpreference.library as library';

var callStatus = rpc.declare({ object: 'netpreference', method: 'status', expect: {} });
var callDevices = rpc.declare({ object: 'netpreference', method: 'devices', expect: {} });
var callTraffic = rpc.declare({ object: 'netpreference', method: 'traffic', expect: {} });
var callApply = rpc.declare({ object: 'netpreference', method: 'apply', expect: {} });
var callRestore = rpc.declare({ object: 'netpreference', method: 'restore', expect: {} });
var callCheck = rpc.declare({ object: 'netpreference', method: 'check_config', params: ['config'], expect: {} });
var callCommit = rpc.declare({ object: 'uci', method: 'commit', params: ['config'] });

function checked(result) {
 if (result && result.ok === false) throw new Error(result.error || '操作失败');
 return result;
}

function bytes(n) {
 n = Number(n || 0);
 var units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
 var i = 0;
 while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
 return n.toFixed(i ? 2 : 0) + ' ' + units[i];
}

function rate(r) {
 return r && r.valid ? bytes(r.upload_Bps) + '/s ↑  ' + bytes(r.download_Bps) + '/s ↓' : '等待有效采样';
}

function total(t, available) {
 return available && t ? bytes(t.upload_bytes) + ' ↑  ' + bytes(t.download_bytes) + ' ↓' : '不可用';
}

function card(label, value, note) {
 return E('div', {
  'style': 'min-width:150px;flex:1 1 150px;padding:14px 16px;border:1px solid var(--border-color-medium,#d9d9d9);border-radius:8px;background:var(--background-color-high,#fff);box-sizing:border-box;'
 }, [
  E('div', { 'style': 'font-size:12px;opacity:.72;margin-bottom:5px;' }, label),
  E('div', { 'style': 'font-size:22px;font-weight:600;line-height:1.25;' }, String(value)),
  note ? E('div', { 'style': 'font-size:12px;opacity:.68;margin-top:5px;line-height:1.45;' }, note) : ''
 ]);
}

function statusText(s) {
 if (s.active) return '策略已启用';
 if (s.desired) return '故障开放 / 等待 DNS 健康';
 return '空闲 / 未接管设备';
}

return view.extend({
 load: function() {
  return Promise.all([uci.load('netpreference'), callDevices().catch(function() { return { devices: [] }; })]);
 },

 render: function(data) {
  var self = this, known = data[1].devices || [];
  var m = this.map = new form.Map('netpreference', '策略配置',
   '按设备控制 IPv6 / IPv4 DNS 偏好与域名策略。只有加入 NetPreference 的设备才会被接管；未加入设备继续使用原有 dnsmasq → HomeProxy → SmartDNS 链路。DNS 偏好只影响解析结果的时序，不会强制应用程序使用某个协议族。');

  var g = m.section(form.NamedSection, 'main', 'global', '基础设置');
  g.description = '这里控制 NetPreference 自身的监听、上游和流量观察，不会修改 HomeProxy、SmartDNS、NAT66 或 nlbwmon 的原始配置。';

  var o = g.option(form.DynamicList, 'interface', '可信 LAN 接口');
  o.default = 'br-lan'; o.rmempty = false;
  o.description = '只对来自这些 LAN 接口、且已加入策略的设备执行 DNS 重定向。';

  o = g.option(form.Value, 'upstream', '原始 DNS 链上游');
  o.default = '127.0.0.1:53'; o.rmempty = false;
  o.description = '支持 IP:端口、tcp://IP:端口 或 udp://IP:端口；IPv6 地址需要使用方括号。1053 端口由 NetPreference 保留。';

  o = g.option(form.Value, 'timeout_ms', '上游超时（毫秒）');
  o.default = '2000'; o.datatype = 'range(100,5000)'; o.rmempty = false;

  o = g.option(form.Flag, 'monitor', '启用流量观察');
  o.default = '0'; o.rmempty = false;
  o.description = '实时速率和累计流量都直接来自 NetPreference 自己的 nft 计数器，不依赖 nlbwmon。累计值用于粗略判断 IPv4 / IPv6 流量比例；关闭监控或重启服务后从 0 开始；切换模式集不清空累计。启用流量分流/硬件卸载时可能低估。';

  var d = m.section(form.GridSection, 'device', '设备策略');
  d.addremove = true; d.anonymous = true; d.sortable = true;
  d.description = '为指定设备设置双栈、IPv6 优先、IPv4 优先、自定义偏好或 DNS 阻断。高级时序参数在编辑设备时显示。';

  o = d.option(form.Flag, 'enabled', '启用'); o.default = '1'; o.rmempty = false;
  o = d.option(form.Value, 'name', '设备名称'); o.datatype = 'maxlength(80)';
  o = d.option(form.Value, 'mac', '设备 MAC'); o.datatype = 'macaddr'; o.rmempty = false;
  known.forEach(function(h) {
   var ips = (h.ipv4 || []).concat(h.ipv6 || []).join(', ');
   o.value(h.mac, (h.name || h.mac) + (ips ? ' · ' + ips : ''));
  });

  o = d.option(form.ListValue, 'mode', '设备默认策略'); o.default = 'dual'; o.rmempty = false;
  [['dual', '普通双栈'], ['ipv6', 'IPv6 优先'], ['ipv4', 'IPv4 优先'], ['custom', '自定义偏好'], ['block', '阻断 DNS（REFUSED）']].forEach(function(v) { o.value(v[0], v[1]); });

  o = d.option(form.ListValue, 'profile', '模式集');
  o.value('', '不使用模式集'); o.rmempty = true;
  o.description = '仅匹配域名覆盖上面的设备默认策略。未匹配域名仍使用本设备的模式与 A/B。在“模式集与域名集”页面创建和保存模式。';
  uci.sections('netpreference', 'profile', function(s) { o.value(s['.name'], s.name || '未命名模式集'); });

  o = d.option(form.ListValue, 'prefer', '自定义优先协议族');
  o.value('ipv6', 'IPv6'); o.value('ipv4', 'IPv4'); o.default = 'ipv6'; o.depends('mode', 'custom'); o.modalonly = true; o.retain = true;

  o = d.option(form.ListValue, '_preset', '偏好强度预设'); o.modalonly = true;
  o.value('soft', '温和：A=75 ms，B=40 ms');
  o.value('balanced', '均衡：A=120 ms，B=80 ms');
  o.value('strong', '强：A=250 ms，B=150 ms');
  o.value('custom', '使用下方自定义值');
  o.cfgvalue = function(sid) {
   var a = String(uci.get('netpreference', sid, 'wait_ms') || '120');
   var b = String(uci.get('netpreference', sid, 'delay_ms') || '80');
   return a === '75' && b === '40' ? 'soft' : a === '120' && b === '80' ? 'balanced' : a === '250' && b === '150' ? 'strong' : 'custom';
  };
  o.write = function() {}; o.remove = function() {};
  o.onchange = function(ev, sid, value) {
   var p = { soft: [75, 40], balanced: [120, 80], strong: [250, 150] }[value];
   if (!p) return;
   ['wait_ms', 'delay_ms'].forEach(function(key, ix) {
    var opt = m.lookupOption(key, sid)[0], el = opt && opt.getUIElement(sid);
    if (el) el.setValue(String(p[ix]));
   });
  };

  o = d.option(form.Value, 'wait_ms', 'A：等待优先协议族证据（毫秒）');
  o.datatype = 'range(0,750)'; o.default = '120'; o.rmempty = false; o.modalonly = true;
  o.description = '从收到查询开始，最多等待优先协议族结果的时间。若 AAAA 不存在或明确失败，不会因此压制有效的 IPv4 结果。';

  o = d.option(form.Value, 'delay_ms', 'B：确认优先协议族可用后额外延迟（毫秒）');
  o.datatype = 'range(0,500)'; o.default = '80'; o.rmempty = false; o.modalonly = true;
  o.description = 'A+B 总和不能超过 1000 ms；不允许无限等待。';

  o = d.option(form.Flag, 'probe', '主动探测优先记录类型');
  o.default = '1'; o.rmempty = false; o.modalonly = true;
  o.description = 'IPv6 优先时主动探测 AAAA，IPv4 优先时主动探测 A。关闭后，只利用并行或近期查询提供的证据。主动探测不会让只支持 IPv4 的应用自动改用 IPv6。';

  o = d.option(form.Value, 'upstream', '设备专用上游 DNS'); o.modalonly = true;
  o.placeholder = '127.0.0.1:53';
  o.description = '可选。传输失败、SERVFAIL 或 REFUSED 时会回退到原始 DNS 链。';

  this.statusBox = E('div', { 'class': 'cbi-section', 'style': 'margin-bottom:1em;' }, '正在读取运行状态…');
  this.trafficBox = E('div', { 'class': 'cbi-section' });

  var restore = E('button', {
   'class': 'cbi-button cbi-button-reset',
   'click': ui.createHandlerFn(this, function() {
    return callRestore().then(checked).then(function() {
     uci.unload('netpreference');
     return uci.load('netpreference');
    }).then(function() { return self.refresh(); }).catch(function(err) {
     ui.addNotification(null, E('p', {}, err.message || String(err)), 'error');
    });
   })
  }, '恢复原始 DNS / 停用插件');

  poll.add(function() { return self.refresh(); }, 3);

  return m.render().then(function(node) {
   self.refresh();
   return E('div', {}, [
    E('div', { 'class': 'cbi-section' }, [
     E('h2', {}, 'NetPreference 网络偏好'),
     E('p', {}, '按设备管理 DNS 双栈偏好、域名规则与 IPv4 / IPv6 流量观察。默认状态不会接管任何设备。')
    ]),
    self.statusBox,
    E('div', { 'class': 'cbi-section' }, [
     E('h3', {}, '安全恢复'),
     restore,
     E('p', { 'style': 'margin-top:.8em;opacity:.75;' }, '恢复操作只删除 NetPreference 自己创建的规则，不会覆盖 HomeProxy、SmartDNS、防火墙或其它监控服务的现有配置。DoH/DoT、VPN 内 DNS 与客户端自身缓存不属于本插件的 53 端口策略范围。')
    ]),
    node,
    self.trafficBox
   ]);
  });
 },

 refresh: function() {
  var self=this;
  return Promise.all([
   callStatus().then(checked).then(function(s){self.renderStatus(s);}).catch(function(err){
    dom.content(self.statusBox,E('div',{'class':'alert-message error'},'状态读取失败：'+(err.message||String(err))));
   }),
   callTraffic().then(checked).then(function(t){self.renderTraffic(t);}).catch(function(err){
    dom.content(self.trafficBox,E('div',{'class':'alert-message error'},'流量 RPC 读取失败：'+(err.message||String(err))));
   })
  ]);
 },
 renderStatus: function(s) {
   var state = statusText(s);
   var stateNote = s.active ? '已选设备的 DNS 策略正在工作' : s.desired ? '为避免断网，当前保持故障开放' : '后台服务运行，但没有启用策略';

   dom.content(this.statusBox, [
    E('div', { 'style': 'display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;margin-bottom:12px;' }, [
     E('div', {}, [
      E('h3', { 'style': 'margin:0;' }, '运行概览'),
      E('div', { 'style': 'opacity:.7;margin-top:4px;' }, 'NetPreference ' + (s.version || ''))
     ]),
     E('div', { 'style': 'font-weight:600;' }, state)
    ]),
    E('div', { 'style': 'display:flex;gap:10px;flex-wrap:wrap;' }, [
     card('运行状态', state, stateNote),
     card('已选设备', s.selected_devices || 0, '当前受策略管理的设备数'),
     card('DNS 查询', s.queries || 0, 'NetPreference 已处理查询'),
     card('回退次数', s.fallbacks || 0, '回退到原始 DNS 链')
    ]),
    (s.error || s.discovery_warning) ? E('div', {
     'class': 'alert-message warning',
     'style': 'margin-top:12px;'
    }, s.error || s.discovery_warning) : ''
   ]);

 },
 renderTraffic: function(t) {
   if (!t.enabled) {
    dom.content(this.trafficBox, [
     E('h3', {}, '流量观察'),
     E('p', {}, '流量观察当前关闭。NetPreference 不会采样或累计流量；其它系统服务不受影响。')
    ]);
    return;
   }

   var table = E('table', { 'class': 'table' }, E('tr', { 'class': 'tr table-titles' },
    ['设备', 'IPv4 实时', 'IPv6 实时', 'IPv4 会话累计', 'IPv6 会话累计', 'IPv6 占比'].map(function(h) {
     return E('th', { 'class': 'th' }, h);
    })));

   (t.rows || []).forEach(function(row) {
    var h = row.host || {}, ip = (h.ipv4 || []).concat(h.ipv6 || []).join(', ');
    table.appendChild(E('tr', { 'class': 'tr' }, [
     E('td', { 'class': 'td', 'title': ip }, [E('strong', {}, h.name || h.mac), E('br'), E('small', {}, h.mac)]),
     E('td', { 'class': 'td' }, rate(row.ipv4_rate)),
     E('td', { 'class': 'td' }, rate(row.ipv6_rate)),
     E('td', { 'class': 'td' }, total((row.totals || {}).ipv4, row.ipv4_available === undefined ? t.totals_available : row.ipv4_available)),
     E('td', { 'class': 'td' }, total((row.totals || {}).ipv6, row.ipv6_available === undefined ? t.totals_available : row.ipv6_available)),
     E('td', { 'class': 'td' }, (row.ipv4_available === undefined ? t.totals_available : row.ipv4_available && row.ipv6_available) ? (100 * (row.ipv6_ratio || 0)).toFixed(1) + '%' : '—')
    ]));
   });

   var body = [
    E('h3', {}, '流量观察'),
    E('p', {}, '实时速率与会话累计都来自 NetPreference 自己的 LAN 边界 nft 计数器（包含路由器本地代理通信）。显示设备侧协议族，不等于代理的 WAN 出口协议族。会话累计从监控开始后的相邻采样字节差持续相加，仅用于粗略判断 IPv4 / IPv6 流量比例；关闭监控或服务重启后从 0 开始；切换模式或计数器重建保留已采样的累计，缺失区间不估算。')
   ];
   if (t.state === 'warming_up') body.push(E('p', {}, '监控已开启，正在建立速率基线，等待下一次采样。'));
   if (t.caveat || t.error) body.push(E('p', { 'style': 'opacity:.75;' }, (t.caveat || '') + ' ' + (t.error || '')));
   if ((t.rows || []).length) body.push(table);
   else body.push(E('p', {}, '暂时没有可显示的设备流量数据。'));
   dom.content(this.trafficBox, body);
 },

 handleSaveApply: function() {
  var self = this;
  return this.map.save(function() { return callCheck(library.serialize(uci)).then(checked); }).then(function() { return callCommit('netpreference'); }).then(function(code) {
   if (typeof code === 'number' && code !== 0) throw new Error('UCI 提交失败：' + code);
   return callApply();
  }).then(checked).then(function() {
   ui.addNotification(null, E('p', {}, 'NetPreference 配置已保存并应用。'), 'info');
   return self.refresh();
  }).catch(function(err) {
   ui.addNotification(null, E('p', {}, err.message || String(err)), 'error');
  });
 },

 handleSave: null
});
