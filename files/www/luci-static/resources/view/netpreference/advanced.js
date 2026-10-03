'use strict';
'require view';
'require form';
'require rpc';
'require uci';
'require ui';

var callApply = rpc.declare({ object: 'netpreference', method: 'apply', expect: {} });
var callValidate = rpc.declare({ object: 'netpreference', method: 'validate', expect: {} });
var callCommit = rpc.declare({ object: 'uci', method: 'commit', params: ['config'] });
function checked(r) {
 if (r && r.ok === false) throw new Error(r.error || 'Validation failed');
 return r;
}
function reference(section, key, title, type, optional) {
 var o = section.option(form.ListValue, key, title);
 o.rmempty = optional; o.optional = optional;
 // Rebuild on each render, including after adding a named section in this form.
 o.load = function(sid) {
  this.keylist = []; this.vallist = [];
  this.value('', optional ? '不指定' : '请选择');
  var self = this;
  uci.sections('netpreference', type, function(s) {
   self.value(s['.name'], (s.name || s['.name']) + ' [' + s['.name'] + ']');
  });
  return form.ListValue.prototype.load.call(this, sid);
 };
 o.validate = function(sid, value) {
  if (!value) return optional ? true : '必须选择一项';
  var target = uci.get('netpreference', value);
  return target && target['.type'] === type ? true : '引用不存在，请先创建并保存';
 };
 return o;
}
function domainValid(value) {
 value = (value || '').trim().toLowerCase();
 if (value === '*.') return false;
 value = value.replace(/\.$/, '');
 if (value === '*') return true;
 value = value.replace(/^\*\./, '');
 return value.length > 0 && value.length <= 253 && value.split('.').every(function(label) {
  return /^[a-z0-9_-]{1,63}$/.test(label);
 });
}
function selector(section) {
 // Both fields stay visible: no hidden dependency can delete an old selector.
 var o = section.option(form.Value, 'domain', '直接填写域名');
 o.placeholder = '*.example.com'; o.optional = true;
 o.description = '与域名集二选一。*.example.com 包含根域；* 匹配所有域名。国际化域名请使用 punycode。';
 o.validate = function(sid, value) { return !value || domainValid(value) ? true : '域名格式无效'; };
 reference(section, 'domain_set', '或选择命名域名集', 'domain_set', true);
}
function modes(o) {
 [['inherit','继承设备默认'],['dual','普通双栈'],['ipv6','IPv6 优先'],['ipv4','IPv4 优先'],['custom','自定义偏好']].forEach(function(v) { o.value(v[0],v[1]); });
}
function enabled(section) {
 var o = section.option(form.Flag,'enabled','启用'); o.default='1'; o.rmempty=false;
}
// Validate the whole pending library before committing. In particular deleting
// a referenced profile/set must not leave the saved file unbootable. Backend
// ParseUCI is still authoritative and independently validates every Apply.
function validateReferences() {
 var profiles = {}, sets = {}, devices = [];
 uci.sections('netpreference','profile',function(s){profiles[s['.name']]=true;});
 uci.sections('netpreference','domain_set',function(s){sets[s['.name']]=true;});
 uci.sections('netpreference','device',function(s){devices.push(s);});
 devices.forEach(function(d){if(d.profile && !profiles[d.profile])throw new Error('Device '+d['.name']+': unknown profile '+d.profile);});
 ['policy','rule'].forEach(function(type){
  uci.sections('netpreference',type,function(s){
   if ((type==='policy' && !s.profile) || (s.profile && !profiles[s.profile])) throw new Error(s['.name']+': unknown profile');
   if (!!s.domain === !!s.domain_set) throw new Error(s['.name']+': domain / domain_set must have exactly one value');
   if(s.domain_set && !sets[s.domain_set]) throw new Error(s['.name']+': unknown domain_set');
   if(type==='policy') {
    var bases=devices.filter(function(d){return d.profile===s.profile;});bases.push({});
    bases.forEach(function(d){
     var a=Number(s.wait_ms!==undefined && s.wait_ms!=='' ? s.wait_ms : (d.wait_ms!==undefined ? d.wait_ms : 120));
     var b=Number(s.delay_ms!==undefined && s.delay_ms!=='' ? s.delay_ms : (d.delay_ms!==undefined ? d.delay_ms : 80));
     if(a<0||a>750||b<0||b>500||a+b>1000)throw new Error(s['.name']+': inherited A+B must be <=1000 ms');
    });
   }
  });
 });
}

return view.extend({
 load: function() { return uci.load('netpreference'); },
 render: function() {
  var m = this.map = new form.Map('netpreference','高级模式与域名集',
   '先创建命名 Profile 和域名集，再添加域名覆盖。保存后回到设备页选择 Profile。每台设备始终保留自己的默认策略；没有匹配的域名不受高级规则影响。');
  var p = m.section(form.GridSection,'profile','1. 命名模式 / Profiles');
  p.anonymous=false;p.addremove=true;
  p.description='新建时输入稳定 ID（1–64 位字母、数字或下划线），显示名称可以使用中文。一个模式可由多台设备共用；删除前需先移除引用。';
  var o=p.option(form.Value,'name','显示名称');o.rmempty=false;o.datatype='maxlength(80)';
  o=p.option(form.Value,'description','备注');o.datatype='maxlength(256)';

  var ds=m.section(form.GridSection,'domain_set','2. 命名域名集 / DomainSets');
  ds.anonymous=false;ds.addremove=true;
  ds.description='使用稳定 ID。一行一个域名，支持精确域名、*.example.com 和 *；集合可跨 Profile 共用，不支持递归引用。';
  o=ds.option(form.Value,'name','显示名称');o.rmempty=false;o.datatype='maxlength(80)';
  o=ds.option(form.DynamicList,'domain','域名列表');o.rmempty=false;o.placeholder='*.example.com';
  o.validate=function(sid,v){
   var values=Array.isArray(v)?v:[v];
   return values.length>0 && values.length<=256 && values.every(domainValid) ? true : '请输入 1–256 个有效域名';
  };

  var a=m.section(form.GridSection,'policy','3. 域名偏好覆盖');
  a.anonymous=true;a.addremove=true;a.sortable=true;
  a.description='最具体的域名匹配优先，同分时列表靠前者优先。只选一条规则；留空的 A/B 和探测开关直接继承设备，不从其他规则叠加。';
  enabled(a);reference(a,'profile','Profile','profile',false);selector(a);
  o=a.option(form.ListValue,'mode','偏好模式');modes(o);o.default='inherit';o.rmempty=false;
  o=a.option(form.ListValue,'prefer','自定义首选协议族');o.value('','继承设备');o.value('ipv6','IPv6');o.value('ipv4','IPv4');o.modalonly=true;o.retain=true;o.depends('mode','custom');
  o=a.option(form.Value,'wait_ms','A：等待证据（ms）');o.placeholder='继承设备';o.optional=true;o.datatype='range(0,750)';o.modalonly=true;
  o.description='留空为继承，0 为不等待。';
  o=a.option(form.Value,'delay_ms','B：额外延迟（ms）');o.placeholder='继承设备';o.optional=true;o.datatype='range(0,500)';o.modalonly=true;
  o.description='确认首选记录存在才生效。继承后 A+B 不得超过 1000 ms。';
  o=a.option(form.ListValue,'probe','主动探测');o.value('','继承设备');o.value('1','启用');o.value('0','关闭');o.modalonly=true;

  var r=m.section(form.GridSection,'rule','4. DNS 动作（兼容旧版规则）');
  r.anonymous=true;r.addremove=true;r.sortable=true;
  r.description='动作与时序分开匹配；偏好规则不会解除阻断。先设备范围，再域名具体度，完全同分时 Profile 规则优先，最后按列表顺序。本地阻断和静态响应立即返回，不附加 A/B。';
  enabled(r);reference(r,'profile','仅在此 Profile 下生效','profile',true);
  o=r.option(form.Value,'device','设备范围（MAC 或 *）');o.default='*';o.rmempty=false;o.value('*','全部已加入设备');
  uci.sections('netpreference','device',function(s){if(s.mac)o.value(s.mac,s.name||s.mac);});
  selector(r);
  o=r.option(form.ListValue,'qtype','查询类型');o.default='*';o.rmempty=false;['*','A','AAAA','HTTPS','SVCB'].forEach(function(v){o.value(v);});
  o=r.option(form.ListValue,'action','DNS 动作');o.default='nxdomain';o.rmempty=false;
  [['forward','转发 / 指定上游'],['rewrite','静态地址重写'],['nxdomain','NXDOMAIN'],['nodata','NODATA'],['refused','REFUSED'],['sinkhole','0.0.0.0 / ::'],['ipv4_only','仅 IPv4（AAAA NODATA）'],['ipv6_only','仅 IPv6（A NODATA）']].forEach(function(v){o.value(v[0],v[1]);});
  o=r.option(form.DynamicList,'ipv4','静态 IPv4');o.datatype='ip4addr';o.depends('action','rewrite');o.modalonly=true;o.retain=true;
  o=r.option(form.DynamicList,'ipv6','静态 IPv6');o.datatype='ip6addr';o.depends('action','rewrite');o.modalonly=true;o.retain=true;
  o=r.option(form.Value,'upstream','规则上游 DNS');o.placeholder='127.0.0.1:53';o.depends('action','forward');o.modalonly=true;o.retain=true;
  o=r.option(form.Value,'ttl','TTL（秒）');o.datatype='range(0,86400)';o.default='60';o.rmempty=false;o.modalonly=true;
  return m.render();
 },
 saveLibrary: function(apply) {
  return this.map.save().then(function(){validateReferences();return callCommit('netpreference');}).then(function(code){
   if(typeof code==='number'&&code!==0)throw new Error('UCI commit failed: '+code);
   return callValidate();
  }).then(checked).then(function(){return apply ? callApply().then(checked) : null;}).then(function(){
   ui.addNotification(null,E('p',{},apply ? '模式库已保存并应用。' : '模式库已保存。运行中的策略尚未切换，请到设备页选择 Profile 后应用。'),'info');
  }).catch(function(err){ui.addNotification(null,E('p',{},err.message||String(err)),'error');});
 },
 handleSave: function(){return this.saveLibrary(false);},
 handleSaveApply: function(){return this.saveLibrary(true);}
});
