'use strict';
// Documented LuCI API doubles. Shared by Node contract tests and Chromium
// interaction tests. Not a substitute for the target router's LuCI acceptance.
(function(global){
 function create(E, validate){
  const calls=[], notices=[], values={};
  let sections={
   main:{'.name':'main','.type':'global',enabled:'1',monitor:'1',interface:['br-lan']},
   pc:{'.name':'pc','.type':'device',mac:'02:00:00:00:00:01',name:'Laptop',profile:'work',mode:'ipv4',wait_ms:'120',delay_ms:'80'},
   work:{'.name':'work','.type':'profile',name:'Work',description:'Retain this note'},
   media:{'.name':'media','.type':'domain_set',name:'Media',domain:['*.media.test','exact.test']},
   bias:{'.name':'bias','.type':'policy',profile:'work',domain_set:'media',mode:'ipv6',wait_ms:'0',delay_ms:'0',probe:'0'},
   block:{'.name':'block','.type':'rule',domain:'bad.test',device:'*',action:'nxdomain',custom_note:'retained'}
  };
  let saved=JSON.parse(JSON.stringify(sections));
  const responses={
   status:{version:'0.2.0',active:true,desired:true,monitoring:true,selected_devices:1,queries:4},
   devices:{devices:[{mac:'02:00:00:00:00:01',name:'Laptop',ipv4:['192.0.2.2'],ipv6:['fd42::2']}]},
   traffic:{enabled:true,state:'ready',totals_available:true,rows:[{host:{mac:'02:00:00:00:00:01',name:'Laptop',ipv4:[],ipv6:[]},totals:{ipv4:{upload_bytes:100,download_bytes:200},ipv6:{upload_bytes:300,download_bytes:400}},ipv4_rate:{valid:true,upload_Bps:50,download_Bps:70},ipv6_rate:{valid:false},ipv6_ratio:0.7}]},
   apply:{ok:true},restore:{ok:true},validate:{valid:true},check_config:{ok:true,valid:true},commit:0
  };
  const rpc={declare(spec){return async(...args)=>{
   calls.push([spec.object,spec.method,...args]);
   if(spec.method==='check_config'&&validate)return validate(args[0]);
   if(responses[spec.method] instanceof Error)throw responses[spec.method];
   if(spec.method==='commit'&&responses.commit===0)saved=JSON.parse(JSON.stringify(sections));
   return responses[spec.method];
  };}};
  const uci={
   async load(){return {};},unload(){sections=JSON.parse(JSON.stringify(saved));},
   get(p,s,k){return k==null?sections[s]:(sections[s]||{})[k];},
   sections(p,type,cb){const rows=Object.values(sections).filter(s=>!type||s['.type']===type);if(cb)rows.forEach(cb);return rows;},
   add(p,type,name){if(!name)throw new Error('Library must create stable named sections');sections[name]={'.name':name,'.type':type};return name;},
   set(p,s,k,v){if(v==null||v==='')delete sections[s][k];else sections[s][k]=v;},
   remove(p,s){delete sections[s];},
   move(p,s,target){const row=sections[s];if(!row)return false;delete sections[s];sections[s]=row;return true;},
   async save(){calls.push(['uci','save']);}
  };
  class Option{
   constructor(section,name){this.section=section;this.map=section.map;this.name=name;this.option=name;this.choices=[];this.keylist=[];this.vallist=[];}
   value(k,v){this.choices.push([k,v]);this.keylist.push(k);this.vallist.push(v||k);}
   depends(){} load(sid){return uci.get('netpreference',sid,this.name);}
   getUIElement(sid){return {setValue:v=>{values[`${sid}/${this.name}`]=v;}};}
  }
  class Section{
   constructor(map,type){this.map=map;this.sectiontype=type;this.options=[];}
   option(Type,name){const o=new Type(this,name);this.options.push(o);this.map.options.push(o);return o;}
  }
  class FormMap{
   constructor(){this.options=[];this.sections=[];}
   section(_Type,type){const s=new Section(this,type);this.sections.push(s);return s;}
   lookupOption(name,sid){return this.options.filter(o=>o.name===name&&(!sections[sid]||o.section.sectiontype===sections[sid]['.type']));}
   async render(){for(const o of this.options)for(const s of Object.values(sections).filter(s=>s['.type']===o.section.sectiontype))await o.load(s['.name']);return E('div',{'class':'test-luci-form'},'LuCI form API fixture');}
   async save(cb){calls.push(['form','save']);if(cb)await cb();await uci.save();}
   async reset(){return this.render();}
  }
  const form={Map:FormMap,NamedSection:Section,GridSection:Section,Value:Option,Flag:Option,DynamicList:Option,ListValue:Option};
  const ui={createHandlerFn(self,fn){return fn.bind(self);},addNotification(a,msg,type){notices.push([type,msg]);}};
  const poll={add(fn){this.fn=fn;}},dom={content(node,children){
   if(node.replaceChildren){node.replaceChildren();(Array.isArray(children)?children:[children]).forEach(c=>node.append(c));}
   else node.children=children;
  }};
  const h={calls,notices,values,responses,uci,form,ui,poll,dom,E,baseclass:{extend:x=>x},view:{extend:x=>x},get sections(){return sections;}};
  h.module=function(source){return new Function('baseclass',source)(h.baseclass);};
  h.loadView=function(source,library){return new Function('view','form','rpc','uci','ui','poll','dom','E','_','library',source)(h.view,form,rpc,uci,ui,poll,dom,E,x=>x,library);};
  h.option=function(v,type,name){return v.map.sections.find(s=>s.sectiontype===type).options.find(o=>o.name===name);};
  return h;
 }
 global.NPTest={create:create};
 if(typeof module!=='undefined')module.exports=global.NPTest;
})(typeof window!=='undefined'?window:globalThis);
