'use strict';
// Exercise the actual LuCI module with small API doubles, not a visual browser.
const assert = require('assert');
const fs = require('fs');
const path = require('path');
const calls = [];
function E(tag, attrs, children) {
 const node = { tag, attrs: attrs || {}, children: children || [], appendChild(child) {
  if (!Array.isArray(this.children)) this.children = [this.children]; this.children.push(child);
 }};
 return node;
}
const responses = {
 status: { version: '0.1.0', active: true, selected_devices: 1, queries: 4 },
 devices: { devices: [{mac:'02:00:00:00:00:01',name:'laptop',ipv4:['192.0.2.2'],ipv6:['fd42::2']}] },
 traffic: { enabled: true, totals_available: true, rows: [{host:{mac:'02:00:00:00:00:01',name:'laptop',ipv4:[],ipv6:[]},
  totals:{ipv4:{upload_bytes:100,download_bytes:200},ipv6:{upload_bytes:300,download_bytes:400}},
  ipv4_rate:{valid:true,upload_Bps:50,download_Bps:70},ipv6_rate:{valid:false},ipv6_ratio:0.7}] },
 apply: {ok:true}, restore: {ok:true}, commit:0
};
const rpc = { declare(spec) { return async (...args) => { calls.push([spec.object,spec.method,...args]); return responses[spec.method]; }; } };
const values = {};
class Option {
 constructor(name) { this.name=name; this.choices=[]; }
 value(...args){this.choices.push(args);}
 depends(){}
 getUIElement(sid){return {setValue:v=>{values[`${sid}/${this.name}`]=v;}};}
}
class Section {
 constructor(map){this.map=map;}
 option(_type,name){const o=new Option(name);this.map.options.push(o);return o;}
}
class FormMap {
 constructor(){this.options=[];}
 section(){return new Section(this);}
 lookupOption(name){return this.options.filter(o=>o.name===name);}
 async render(){return E('form');}
 async save(){calls.push(['form','save']);}
}
const form={Map:FormMap,NamedSection:Section,GridSection:Section,Value:Option,Flag:Option,DynamicList:Option,ListValue:Option};
const uci={async load(){return {};},unload(){},get(){return undefined;},sections(_p,_s,cb){cb({mac:'02:00:00:00:00:01',name:'laptop'});}};
const notices=[];
const ui={createHandlerFn(self,fn){return fn.bind(self);},addNotification(_a,msg,type){if(type==='error')throw new Error(JSON.stringify(msg));notices.push([type,msg]);}};
const poll={add(fn){this.fn=fn;}};
const dom={content(node,children){node.children=children;}};
const source=fs.readFileSync(path.join(__dirname,'../files/www/luci-static/resources/view/netpreference/overview.js'),'utf8');
const moduleView=new Function('view','form','rpc','uci','ui','poll','dom','E','_',source)({extend:x=>x},form,rpc,uci,ui,poll,dom,E,x=>x);
(async()=>{
 const data=await moduleView.load();await moduleView.render(data);await moduleView.refresh();
 assert(JSON.stringify(moduleView.trafficBox).includes('70.0%'));
 const preset=moduleView.map.lookupOption('_preset')[0];preset.onchange(null,'device1','strong');
 assert.equal(values['device1/wait_ms'],'250');assert.equal(values['device1/delay_ms'],'150');
 calls.length=0;await moduleView.handleSaveApply();
 assert.deepEqual(calls.slice(0,3),[['form','save'],['uci','commit','netpreference'],['netpreference','apply']]);
 assert(!calls.some(x=>x[0]==='uci'&&x[1]==='apply'),'must not commit unrelated pending UCI packages');
 assert(notices.some(n=>n[0]==='info'&&JSON.stringify(n[1]).includes('NetPreference 配置已保存并应用')));
 assert.equal(moduleView.handleSave,null);
 const actions=moduleView.map.lookupOption('action')[0].choices.map(v=>v[0]);
 assert(actions.includes('rewrite')&&actions.includes('nxdomain')&&actions.includes('ipv4_only'));
 responses.traffic={enabled:false};await moduleView.refresh();
 assert(JSON.stringify(moduleView.trafficBox).includes('nlbwmon')&&JSON.stringify(moduleView.trafficBox).includes('NetPreference'));
 console.log('PASS: LuCI render/status/traffic/preset and own-package-only Save & Apply contract (API doubles, not browser validation).');
})().catch(e=>{console.error(e);process.exit(1);});
