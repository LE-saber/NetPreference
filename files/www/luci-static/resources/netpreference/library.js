'use strict';
'require baseclass';

// UI model only: the daemon's existing ParseUCI and matching engine remain the
// authority. IDs are generated once, persisted as named UCI sections and never
// derived from display names (renaming cannot break references).
var config = 'netpreference', serial = 0;
function list(value) { return value == null ? [] : Array.isArray(value) ? value.slice() : [value]; }
function display(pattern) { return String(pattern || '').replace(/^\*\./, ''); }
function domain(input) {
 var value = String(input || '').trim().replace(/[。．｡]/g, '.');
 if (value === '*') return '*';
 if (!value || value === '*.') throw new Error('请输入域名');
 value = value.replace(/^\*\./, '').replace(/^\./, '');
 // Accept a pasted website address as well as a hostname; never permit
 // arbitrary URL schemes, credentials, ports in the stored DNS pattern.
 if (/^[a-z][a-z0-9+.-]*:\/\//i.test(value) && !/^https?:\/\//i.test(value))
  throw new Error('请输入域名或 HTTP(S) 网址');
 var url;
 try { url = new URL(/^https?:\/\//i.test(value) ? value : 'https://' + value); }
 catch (_) { throw new Error('无法识别域名：' + input); }
 if (url.username || url.password || /[\s*\\]/.test(value)) throw new Error('域名不应包含空格、账号或通配符碎片');
 value = url.hostname.toLowerCase().replace(/\.$/, '');
 if (!value || value.length > 253 || !value.split('.').every(function(s) { return /^[a-z0-9_-]{1,63}$/.test(s); }))
  throw new Error('域名格式无效：' + input);
 return '*.' + value;
}
function domains(text) {
 var tokens = String(text || '').replace(/^\s*#.*$/gm, '').split(/[\s,;，；、]+/).filter(Boolean);
 var unique = [];
 tokens.forEach(function(t) { var p = domain(t); if (unique.indexOf(p) < 0) unique.push(p); });
 if (!unique.length || unique.length > 256) throw new Error('每个域名集需要 1–256 个域名');
 return unique;
}
function id(uci, model, prefix) {
 var used = Object.create(null);
 uci.sections(config).forEach(function(s) { used[s['.name']] = true; });
 model.sets.concat(model.profiles).forEach(function(s) { used[s.id] = true; });
 model.profiles.forEach(function(p) { p.rows.forEach(function(r) { used[r.id] = true; }); });
 var key;
 do { key = prefix + '_' + Date.now().toString(36) + '_' + (++serial).toString(36); } while (used[key]);
 return key;
}
function load(uci) {
 var m = {sets: [], profiles: [], original: []};
 uci.sections(config, 'domain_set').forEach(function(s) {
  var originals = list(s.domain);
  m.sets.push({id:s['.name'], name:s.name || '', original:originals, text:originals.map(display).join('\n'), dirty:false});
  m.original.push(s['.name']);
 });
 uci.sections(config, 'profile').forEach(function(s) {
  var p = {id:s['.name'], name:s.name || '', rows:[]};
  uci.sections(config, 'policy').filter(function(r) { return r.profile === p.id; }).forEach(function(r) {
   p.rows.push({id:r['.name'], kind:r.domain_set ? 'set' : 'domain', domain_set:r.domain_set || '', input:display(r.domain), original:r.domain || '', dirty:false,
    mode:r.mode || 'inherit', prefer:r.prefer || '', wait_ms:r.wait_ms == null ? '' : r.wait_ms, delay_ms:r.delay_ms == null ? '' : r.delay_ms,
    probe:r.probe == null ? '' : r.probe, enabled:r.enabled == null ? '1' : r.enabled});
   m.original.push(r['.name']);
  });
  m.profiles.push(p); m.original.push(p.id);
 });
 return m;
}
function newSet(uci,m) { var s={id:id(uci,m,'ds'), name:'', text:'', original:[], dirty:true}; m.sets.push(s); return s; }
function newProfile(uci,m) { var p={id:id(uci,m,'pf'),name:'',rows:[]};m.profiles.push(p);return p; }
function newRow(uci,m,p) {
 var r={id:id(uci,m,'pref'),kind:'domain',input:'',original:'',dirty:true,domain_set:'',mode:'ipv6',prefer:'',wait_ms:'',delay_ms:'',probe:'',enabled:'1'};
 p.rows.push(r);return r;
}
function nameCheck(items, label) {
 var names = Object.create(null);
 items.forEach(function(item) {
  var name = item.name.trim(), key = name.toLowerCase();
  if (!name || Array.from(name).length > 80 || /[\x00-\x1f\x7f]/.test(name)) throw new Error(label + '：请填写 1–80 字的名称');
  if (names[key]) throw new Error(label + '名称重复：' + name);
  names[key] = true;
 });
}
function stage(uci,m) {
 nameCheck(m.sets,'域名集');nameCheck(m.profiles,'模式集');
 if (m.sets.length>128 || m.profiles.length>64) throw new Error('域名集或模式集数量超出限制');
 var keep = Object.create(null), operations = [], rows = 0;
 m.sets.forEach(function(s) {keep[s.id]=true;operations.push([s.id,'domain_set',{name:s.name.trim(),domain:s.dirty?domains(s.text):s.original.slice()}]);});
 m.profiles.forEach(function(p) {
  keep[p.id]=true;operations.push([p.id,'profile',{name:p.name.trim()}]);
  p.rows.forEach(function(r) {
   rows++; keep[r.id]=true;
   if(r.kind==='set' && !m.sets.some(function(s){return s.id===r.domain_set;})) throw new Error(p.name+'：请选择已有域名集');
   operations.push([r.id,'policy',{profile:p.id,enabled:r.enabled,domain:r.kind==='domain'?(r.dirty?domain(r.input):r.original):'',domain_set:r.kind==='set'?r.domain_set:'',
    mode:r.mode,prefer:r.prefer,wait_ms:r.wait_ms,delay_ms:r.delay_ms,probe:r.probe}]);
  });
 });
 if(rows>512)throw new Error('域名规则最多 512 条');
 var removing = m.original.filter(function(key) {return !keep[key];});
 uci.sections(config).forEach(function(s) {
  if (removing.indexOf(s['.name'])>=0) return;
  // Policies edited by this model will be checked in their new form below.
  if (s['.type']==='policy' && keep[s['.name']])return;
  ['profile','domain_set'].forEach(function(k) {
   if(s[k] && removing.indexOf(s[k])>=0)throw new Error('仍有设备或 DNS 动作在使用，请先取消引用再删除');
  });
 });
 removing.forEach(function(key){uci.remove(config,key);});
 operations.forEach(function(op) {
  if(!uci.get(config,op[0]))uci.add(config,op[1],op[0]);
  Object.keys(op[2]).forEach(function(k){uci.set(config,op[0],k,op[2][k]===''?null:op[2][k]);});
 });
 // Only the relative order of policy rows changes; DNS actions keep their own
 // order. Anchor each row before its successor instead of repeatedly moving
 // rows to the end of the whole UCI package. The latter is ambiguous across
 // real LuCI uci.js save/reload cycles because profiles, sets and DNS actions
 // share the same section order.
 m.profiles.forEach(function(p) {
  for (var i=p.rows.length-2;i>=0;i--) {
   if(!uci.move(config,p.rows[i].id,p.rows[i+1].id,false))
    throw new Error('无法保存模式集规则顺序');
  }
 });
 // A rejected preflight leaves only local UCI changes. Remember these IDs so
 // removing a newly added item before retry also removes its staged section.
 m.original = Array.from(new Set(m.original.concat(Object.keys(keep))));
}
function quote(value) {
 var text=String(value);
 if(/[\x00\r\n]/.test(text))throw new Error('配置值不能包含换行或 NUL');
 return "'"+text.replace(/'/g,"'\\''")+"'";
}
function serialize(uci) {
 var lines = ["package netpreference"];
 uci.sections(config).forEach(function(s) {
  // get() merges option deletions; sections() alone can still expose deleted
  // options on supported LuCI versions. Validate exactly what will be saved.
  s = uci.get(config, s['.name']) || s;
  lines.push('config '+quote(s['.type'])+' '+quote(s['.name']));
  Object.keys(s).filter(function(k){return k[0]!=='.';}).forEach(function(k){
   list(s[k]).forEach(function(v){lines.push((Array.isArray(s[k])?' list ':' option ')+quote(k)+' '+quote(v));});
  });
 });
 return lines.join('\n')+'\n';
}
return baseclass.extend({domain:domain,domains:domains,display:display,load:load,newSet:newSet,newProfile:newProfile,newRow:newRow,stage:stage,serialize:serialize});
