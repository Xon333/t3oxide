import test from 'node:test';
import assert from 'node:assert/strict';
import {threadList,threadType} from '../adapter/navigation.mjs';
import {lineage} from '../adapter/models.mjs';
function thread(id,type=null,parent=null,status='idle') {return {id,title:id,projectId:'project',status,updatedAt:'2026-10-22T12:00:00Z',
 modelSelection:{model:'fixture'},lineage:{relationshipToParent:type,parentThreadId:parent,rootThreadId:parent??id}};}
function shell(threads){return {threads,projects:[{id:'project',title:'Project'}]};}
function ids(v){return v.threads.map(t=>t.id);}
test('thread types derive solely from explicit lineage, never titles or fork presence',()=>{
  const main=thread('subagent named by human');assert.equal(threadType(main),'main');
  assert.equal(threadType(thread('child','subagent','main')),'subagent');assert.equal(threadType(thread('fork','fork','main')),'fork');
  assert.equal(threadType({id:'anything',forkedFrom:{threadId:'main'}}),'unknown');
  assert.equal(threadType({id:'anything',lineage:{}}),'unknown');assert.equal(lineage({parentThreadId:null}),null);
  assert.equal(threadType(thread('orphan','subagent',null)),'subagent');
  assert.equal(threadType(thread('future','future','main')),'unknown');
});
test('main threads first; collapsed subagents; forks remain separate roots',()=>{
  const data=shell([thread('child','subagent','main','running'),thread('fork','fork','main'),thread('main'),thread('z-main')]);
  const closed=threadList(data,'',{grouped:true});assert.deepEqual(ids(closed),['main','z-main','fork']);
  assert.equal(closed.threads[0].childCount,1);assert.equal(closed.threads[0].expanded,false);
  const open=threadList(data,'',{grouped:true,expanded:['main']});assert.deepEqual(ids(open),['main','child','z-main','fork']);
  assert.equal(open.threads[1].depth,1);assert.equal(open.threads[1].parentTitle,'main');assert.equal(open.threads.at(-1).depth,0);
});
test('nested subagents expand independently without conflating forks',()=>{
 const data=shell([thread('main'),thread('a','subagent','main'),thread('b','subagent','a'),thread('fork','fork','a'),thread('fc','subagent','fork')]);
 assert.deepEqual(ids(threadList(data,'',{grouped:true,expanded:['main']})),['main','a','fork']);
 const all=threadList(data,'',{grouped:true,expanded:['main','a','fork']});assert.deepEqual(ids(all),['main','a','b','fork','fc']);
 assert.deepEqual(all.threads.map(t=>t.depth),[0,1,2,0,1]);
});
test('all four filters preserve authoritative classification and status values',()=>{
 const rows=[thread('main'),thread('a','subagent','main','running'),thread('b','subagent','main','waiting'),thread('c','subagent','main','completed'),thread('d','subagent','main','failed'),thread('fork','fork','main'),{...thread('unknown'),lineage:null}];
 const data=shell(rows);
 assert.deepEqual(ids(threadList(data,'',{typeFilter:'main',grouped:true})),['main']);
 assert.deepEqual(ids(threadList(data,'',{typeFilter:'forks',grouped:true})),['fork']);
 const subs=threadList(data,'',{typeFilter:'subagents',grouped:true});assert.deepEqual(subs.threads.map(t=>t.status),['running','waiting','completed','failed']);
 assert.equal(subs.threads.every(t=>t.lineage.parentThreadId==='main'),true);
 assert.ok(ids(threadList(data,'',{grouped:true})).includes('unknown'));
});
test('search finds collapsed children and includes parent context without changing filter semantics',()=>{
 const data=shell([thread('root'),thread('child','subagent','root'),thread('needle','subagent','child'),thread('other')]);
 const matches=threadList(data,'needle',{grouped:true});assert.deepEqual(ids(matches),['root','child','needle']);
 assert.deepEqual(matches.threads.map(t=>t.contextOnly),[true,true,false]);assert.equal(matches.threads[0].expanded,true);
 assert.deepEqual(ids(threadList(data,'needle',{grouped:true,typeFilter:'subagents'})),['needle']);
 assert.deepEqual(ids(threadList(data,'needle',{grouped:true,typeFilter:'main'})),[]);
});
test('grouping precedes the 200-row limit and reserves roots against subagent flooding',()=>{
 const mains=Array.from({length:120},(_,i)=>thread(`root-${String(i).padStart(3,'0')}`));
 const children=Array.from({length:250},(_,i)=>({...thread(`child-${i}`,'subagent','root-000'),updatedAt:'2026-10-22T13:00:00Z'}));
 const data=shell([...children,...mains]);
 const closed=threadList(data,'',{grouped:true});assert.equal(closed.threads.length,120);assert.equal(closed.threads[0].childCount,250);
 const open=threadList(data,'',{grouped:true,expanded:['root-000']});assert.equal(open.threads.length,200);assert.equal(open.more,true);
 assert.equal(open.threads.filter(t=>t.threadType==='main').length,120);assert.equal(open.threads.filter(t=>t.threadType==='subagent').length,80);
 assert.ok(ids(threadList(data,'child-249',{grouped:true})).includes('child-249'));
});
test('legacy ungrouped list keeps recency order and old shape fields',()=>{
 const a=thread('main'),b={...thread('child','subagent','main'),updatedAt:'2026-10-22T13:00:00Z'};
 assert.deepEqual(ids(threadList(shell([a,b]))),['child','main']);
 const row=threadList(shell([a])).threads[0];for(const field of ['id','title','project','status','model'])assert.equal(typeof row[field],'string');
});
test('list includes exact optional project identity without deriving it from display labels',()=>{
 const t=thread('one');const r=threadList(shell([t])).threads[0];assert.equal(r.projectId,'project');
 delete t.projectId;assert.equal(Object.hasOwn(threadList(shell([t])).threads[0],'projectId'),false);
 t.projectId='bad\u0000project';assert.throws(()=>threadList(shell([t])));
});
test('orphan and cyclic lineages remain bounded and never fabricate a main classification',()=>{
 const data=shell([thread('orphan','subagent','missing'),thread('cycle-a','subagent','cycle-b'),thread('cycle-b','subagent','cycle-a'),thread('self','subagent','self')]);
 const result=threadList(data,'',{grouped:true,expanded:['cycle-a','cycle-b','self']});
 assert.equal(new Set(ids(result)).size,result.threads.length);assert.ok(result.threads.length<=4);
 assert.ok(ids(result).includes('orphan'));assert.ok(ids(result).includes('cycle-a'));assert.ok(ids(result).includes('self'));
 assert.equal(result.threads.every(t=>t.threadType==='subagent'),true);
});
test('archived/deleted parents do not hide an available child; archived rows are excluded',()=>{
 const p={...thread('gone'),archivedAt:'now'},child=thread('child','subagent','gone'),deleted={...thread('deleted'),deletedAt:'now'};
 assert.deepEqual(ids(threadList(shell([p,child,deleted]),'',{grouped:true})),['child']);
});
test('malformed options, duplicate IDs and oversized expansion sets fail closed',()=>{
 const data=shell([thread('one')]);
 assert.throws(()=>threadList(data,'',{typeFilter:'guess'}));assert.throws(()=>threadList(data,'',{expanded:Array(201).fill('one')}));
 assert.throws(()=>threadList(shell([thread('one'),thread('one')])));assert.throws(()=>threadList(data,'',{expanded:['bad\u0000id']}));
});
test('cycle fallbacks cannot exceed the row limit after roots consume most slots',()=>{
 const rows=Array.from({length:195},(_,i)=>thread(`root-${i}`));
 for(let i=0;i<20;i++) rows.push(thread(`cycle-${i}`,'subagent',`cycle-${(i+1)%20}`));
 const r=threadList(shell(rows),'',{grouped:true,expanded:Array.from({length:20},(_,i)=>`cycle-${i}`)});
 assert.equal(r.threads.length,200);assert.equal(r.more,true);assert.equal(r.threads.filter(t=>t.threadType==='main').length,195);
});
test('deep collapsed family and search stay bounded without losing ancestor identity',()=>{
 const rows=[thread('root')];for(let i=0;i<2500;i++)rows.push(thread(`child-${String(i).padStart(4,'0')}`,'subagent',i===0?'root':`child-${String(i-1).padStart(4,'0')}`));
 const data=shell(rows);assert.deepEqual(ids(threadList(data,'',{grouped:true})),['root']);
 const r=threadList(data,'child-2499',{grouped:true});assert.ok(r.threads.length<=200);assert.ok(r.threads.every(t=>t.depth<=12));assert.equal(r.more,true);
});
