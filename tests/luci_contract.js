'use strict';
// API-contract tests run actual view modules with documented LuCI API doubles.
// These are not a browser/real-router acceptance test.
const assert=require('assert'), fs=require('fs'), path=require('path');
const calls=[], notices=[], errors=[], values={};
const sections={
 main:{'.name':'main','.type':'global'},
 pc:{'.name':'pc','.type':'device',mac:'02:00:00:00:00:01',name:'Laptop',profile:'work',mode:'ipv4',wait_ms:'120',delay_ms:'80'},
 work:{'.name':'work','.type':'profile',name:'Work'},
 media:{'.name':'media','.type':'domain_set',name:'Media',domain:['*.media.test']},
 bias:{'.name':'bias','.type':'policy',profile:'work',domain_set:'media',mode:'ipv6',wait_ms:'0',delay_ms:'0',probe:'0'},
 block:{'.name':'block','.type':'rule',domain:'bad.test',device:'*',action:'nxdomain'}
};
function E(tag,attrs,children){return {tag,attrs:attrs||{},children:children||[],appendChild(c){if(!Array.isArray(this.children))this.children=[this.children];this.children.push(c);}};}
const responses={
 status:{version:'0.2.0',active:true,selected_devices:1,queries:4},
 devices:{devices:[{mac:'02:00:00:00:00:01',name:'Laptop',ipv4:['192.0.2.2'],ipv6:['fd42::2']}]},
 traffic:{enabled:true,totals_available:true,rows:[{host:{mac:'02:00:00:00:00:01',name:'Laptop',ipv4:[],ipv6:[]},totals:{ipv4:{upload_bytes:100,download_bytes:200},ipv6:{upload_bytes:300,download_bytes:400}},ipv4_rate:{valid:true,upload_Bps:50,download_Bps:70},ipv6_rate:{valid:false},ipv6_ratio:0.7}]},
 apply:{ok:true},restore:{ok:true},validate:{valid:true},commit:0
};
const rpc={declare(spec){return async(...args)=>{calls.push([spec.object,spec.method,...args]);return responses[spec.method];};}};
const uci={async load(){return {};},unload(){},get(p,s,k){return k==null?sections[s]:(sections[s]||{})[k];},sections(p,type,cb){const rows=Object.values(sections).filter(s=>s['.type']===type);rows.forEach(cb);return rows;}};
class Option {
 constructor(section,name){this.section=section;this.map=section.map;this.name=name;this.option=name;this.choices=[];this.keylist=[];this.vallist=[];}
 value(k,v){this.choices.push([k,v]);this.keylist.push(k);this.vallist.push(v||k);}
 depends(){}
 load(sid){return uci.get('netpreference',sid,this.name);}
 getUIElement(sid){return {setValue:v=>{values[`${sid}/${this.name}`]=v;}};}
}
class Section {
 constructor(map,type){this.map=map;this.sectiontype=type;this.options=[];}
 option(Type,name){const o=new Type(this,name);this.options.push(o);this.map.options.push(o);return o;}
}
class FormMap {
 constructor(){this.options=[];this.sections=[];}
 section(_Type,type){const s=new Section(this,type);this.sections.push(s);return s;}
 lookupOption(name,sid){return this.options.filter(o=>o.name===name&&(!sections[sid]||o.section.sectiontype===sections[sid]['.type']));}
 async render(){for(const o of this.options){for(const s of Object.values(sections).filter(s=>s['.type']===o.section.sectiontype)){await o.load(s['.name']);}}return E('form');}
 async save(){calls.push(['form','save']);}
}
const form={Map:FormMap,NamedSection:Section,GridSection:Section,Value:Option,Flag:Option,DynamicList:Option,ListValue:Option};
const ui={createHandlerFn(self,fn){return fn.bind(self);},addNotification(a,msg,type){(type==='error'?errors:notices).push([type,msg]);}};
const poll={add(fn){this.fn=fn;}}, dom={content(node,children){node.children=children;}};
function view(name){const source=fs.readFileSync(path.join(__dirname,`../files/www/luci-static/resources/view/netpreference/${name}.js`),'utf8');return new Function('view','form','rpc','uci','ui','poll','dom','E','_',source)({extend:x=>x},form,rpc,uci,ui,poll,dom,E,x=>x);}
function option(v,type,name){return v.map.sections.find(s=>s.sectiontype===type).options.find(o=>o.name===name);}
(async()=>{
 const basic=view('overview');await basic.render(await basic.load());await basic.refresh();
 assert(JSON.stringify(basic.trafficBox).includes('70.0%'));
 assert(!basic.map.sections.some(s=>s.sectiontype==='rule'),'advanced rule grid must not crowd basic settings');
 assert(option(basic,'device','profile').choices.some(c=>c[0]==='work'));
 assert(option(basic,'device','mode').choices.some(c=>c[0]==='block'));
 assert(option(basic,'device','prefer').retain,'mode switches must not delete stored custom family');
 const preset=option(basic,'device','_preset');preset.onchange(null,'pc','strong');
 assert.equal(values['pc/wait_ms'],'250');assert.equal(values['pc/delay_ms'],'150');
 calls.length=0;await basic.handleSaveApply();
 assert.deepEqual(calls.slice(0,3),[['form','save'],['uci','commit','netpreference'],['netpreference','apply']]);
 assert.equal(basic.handleSave,null);assert.equal(errors.length,0);
 responses.traffic={enabled:false};await basic.refresh();
 assert(JSON.stringify(basic.trafficBox).includes('NetPreference'));
 const advanced=view('advanced');await advanced.load();await advanced.render();
 for(const type of ['profile','domain_set'])assert.equal(advanced.map.sections.find(s=>s.sectiontype===type).anonymous,false,'referenced IDs must remain stable after UCI save');
 const ref=option(advanced,'policy','profile');
 assert.equal(ref.validate('bias','work'),true,'uci.get(section) returns an OBJECT, not its type');
 assert.notEqual(ref.validate('bias','media'),true);assert.notEqual(ref.validate('bias','missing'),true);
 sections.travel={'.name':'travel','.type':'profile',name:'Travel'};await ref.load('bias');
 assert(ref.keylist.includes('travel'),'new profiles must appear on rerender');
 const probe=option(advanced,'policy','probe');assert.deepEqual(probe.choices.map(v=>v[0]),['','1','0']);
 assert(!option(advanced,'policy','wait_ms').default,'missing override must inherit, not write a default');
 const actions=option(advanced,'rule','action').choices.map(v=>v[0]);
 for(const action of ['rewrite','nxdomain','nodata','refused','sinkhole','forward','ipv4_only','ipv6_only'])assert(actions.includes(action));
 for(const k of ['ipv4','ipv6','upstream'])assert(option(advanced,'rule',k).retain);
 const validateDomain=option(advanced,'policy','domain').validate;
 for(const good of ['', '*','*.example.com','EXAMPLE.COM.'])assert.equal(validateDomain('bias',good),true);
 for(const bad of ['a..b','https://example.com','*.','a/b'])assert.notEqual(validateDomain('bias',bad),true);
 calls.length=0;await advanced.handleSave();
 assert.deepEqual(calls,[['form','save'],['uci','commit','netpreference'],['netpreference','validate']]);
 assert.equal(errors.length,0,'explicit zero and false must be accepted');
 calls.length=0;await advanced.handleSaveApply();
 assert.deepEqual(calls,[['form','save'],['uci','commit','netpreference'],['netpreference','validate'],['netpreference','apply']]);
 const saved={...sections.bias};
 for(const mutation of [{domain:'x.test'}, {profile:'missing'}, {domain_set:'missing'}, {wait_ms:'750',delay_ms:'500'}]){
  sections.bias={...saved,...mutation};calls.length=0;const n=errors.length;
  await advanced.handleSave();assert.equal(errors.length,n+1);assert(!calls.some(c=>c[1]==='commit'),'invalid references/selectors must not be committed');
 }
 sections.bias=saved;
 const activeProfile=sections.work;delete sections.work;calls.length=0;await advanced.handleSave();assert(!calls.some(c=>c[1]==='commit'));sections.work=activeProfile;
 responses.validate={ok:false,error:'backend rejected config'};calls.length=0;await advanced.handleSaveApply();assert(!calls.some(c=>c[1]==='apply'),'backend rejection must not trigger apply');
 assert(!calls.some(x=>x[0]==='uci'&&x[1]==='apply'),'must not apply unrelated packages');
 console.log('PASS: basic/advanced LuCI views, stable references, dynamic selectors, zero/inherit, DNS actions, dangling-reference rejection and scoped save/apply (API doubles).');
})().catch(e=>{console.error(e);process.exit(1);});
