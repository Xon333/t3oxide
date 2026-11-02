import test from 'node:test';
import assert from 'node:assert/strict';
import { restoreBoundedSnapshot } from '../adapter/core.mjs';
const message = {id:'item', type:'assistant_message', messageId:'message', text:'full\r\nbody 🧑‍💻'};
function fixture() {
  return {snapshotSequence:42,historyCursor:'opaque:cursor',hasMoreHistory:true,projection:{
    thread:{id:'thread'},messages:[{id:'message',text:message.text}],checkpoints:[{id:'cp',files:[{path:'a'}]}],
    subagents:[{id:'child',prompt:'prompt',result:'result'}],
    visibleTurnItems:[{sourceThreadId:'parent',visibility:'inherited',item:{id:'inherited',type:'assistant_message',text:'parent body'}},
      {sourceThreadId:'thread',visibility:'local',item:{...message}}],
    turnItems:[{...message},{id:'request',type:'request',requestId:'request-id',input:{id:0,sessionId:1}},
      {id:'checkpoint',type:'checkpoint',checkpointId:'cp',files:[{path:'a'}]},
      {id:'subagent',type:'subagent',subagentId:'child',prompt:'prompt',result:'result'}],
    runtimeRequests:[{id:'request-id'}],runs:[]}};
}
test('ordinary old-host snapshot retains exact contents',()=>{
  const s=fixture(), expected=structuredClone(s); assert.deepEqual(restoreBoundedSnapshot(s),expected);
});
test('exact restoration preserves inherited rows, full bodies, requests and cursors',()=>{
  const expected=fixture(), s=fixture(); s.turnItemsOmitLocalVisible=true;
  s.projection.turnItems.shift();
  const local=s.projection.visibleTurnItems[1].item;
  local.text=''; local.textOmitted=true;
  Object.assign(s.projection.turnItems[1],{files:[],filesOmitted:true});
  Object.assign(s.projection.turnItems[2],{prompt:'',result:null,promptOmitted:true,resultOmitted:true});
  s.ids=['thread','message','item','parent','inherited','request','request-id','cp','checkpoint','child','subagent'];
  const p=s.projection;
  p.thread.id=0;p.messages[0].id=1;p.checkpoints[0].id=7;p.subagents[0].id=9;
  p.visibleTurnItems[0].sourceThreadId=3;p.visibleTurnItems[0].item.id=4;
  p.visibleTurnItems[1].sourceThreadId=0;local.id=2;local.messageId=1;
  p.turnItems[0].id=5;p.turnItems[0].requestId=6;
  p.turnItems[1].id=8;p.turnItems[1].checkpointId=7;
  p.turnItems[2].id=10;p.turnItems[2].subagentId=9;p.runtimeRequests[0].id=6;
  assert.deepEqual(restoreBoundedSnapshot(s),expected);
  assert.deepEqual(restoreBoundedSnapshot(s),expected); // consumed markers are idempotent
});
test('malformed ID tables and missing or marked body sources fail closed',()=>{
  for (const id of [-1,0.5,99]) {
    const s=fixture();s.ids=['thread'];s.projection.thread.id=id;
    assert.throws(()=>restoreBoundedSnapshot(s),{code:'incompatible'});
  }
  for (const kind of ['missing','marked']) {
    const s=fixture();Object.assign(s.projection.turnItems[0],{text:'',textOmitted:true});
    if(kind==='missing')s.projection.messages=[];
    else s.projection.messages[0].textOmitted=true;
    assert.throws(()=>restoreBoundedSnapshot(s),{code:'incompatible'});
  }
});
test('inherited items never acquire a local record body',()=>{
  const s=fixture();Object.assign(s.projection.visibleTurnItems[0].item,{messageId:'message',text:'',textOmitted:true});
  const inherited=structuredClone(s.projection.visibleTurnItems[0]);
  assert.deepEqual(restoreBoundedSnapshot(s).projection.visibleTurnItems[0],inherited);
});

import { T3 } from '../adapter/t3.mjs';
test('bounded decodes before identity checks and retains old-host response compatibility',async()=>{
  for (const compact of [false,true]) {
    const expected=fixture(), s=fixture();
    if(compact){s.ids=['thread'];s.projection.thread.id=0;}
    let requested;
    const backend=new T3({environmentId:'env',baseUrl:'http://127.0.0.1:1'},()=>{});
    backend.http=async path=>{requested=path;return s;};
    assert.deepEqual(await backend.bounded('thread'),expected);
    const url=new URL(requested,'http://fixture');
    assert.equal(url.searchParams.get('compactIds'),'1');
    assert.equal(url.searchParams.get('compactTurnItems'),'1');
    assert.equal(url.searchParams.get('dedupeBodies'),'1');
    assert.equal(url.searchParams.has('deferWork'),false);
  }
});
test('restored wrong source identity is rejected before projection',async()=>{
  const s=fixture();s.ids=['wrong-thread'];s.projection.thread.id=0;
  const backend=new T3({},()=>{});backend.http=async()=>s;
  await assert.rejects(backend.bounded('thread'),{code:'incompatible'});
});
test('legacy snapshots without turnItems remain unchanged',()=>{
  const s=fixture();delete s.projection.turnItems;
  assert.deepEqual(restoreBoundedSnapshot(s),s);
  s.turnItemsOmitLocalVisible=true;
  assert.throws(()=>restoreBoundedSnapshot(s),{code:'incompatible'});
});
test('ID lists expand only at record boundaries; stale body record IDs reject',()=>{
  const s=fixture();s.ids=['native-id',23];
  s.projection.thread.relatedIds=[0,1];
  s.projection.thread.options={sessionId:0};
  restoreBoundedSnapshot(s);
  assert.deepEqual(s.projection.thread.relatedIds,['native-id',23]);
  assert.deepEqual(s.projection.thread.options,{sessionId:0});
  for(const item of [
    {id:'cp-item',type:'checkpoint',checkpointId:'missing',files:[],filesOmitted:true},
    {id:'sub-item',type:'subagent',subagentId:'missing',prompt:'',promptOmitted:true},
    {...message,messageId:'stale',text:'',textOmitted:true},
  ]) {
    const bad=fixture();bad.projection.turnItems.push(item);
    assert.throws(()=>restoreBoundedSnapshot(bad),{code:'incompatible'});
  }
});
