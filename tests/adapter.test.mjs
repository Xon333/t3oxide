import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, writeFile, chmod, symlink, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Fault, LIMIT, clip, hash, boundedRows, trimView, view, requests, command, launch, validateDescriptor } from '../adapter/core.mjs';
import { Rpc } from '../adapter/wire.mjs';
import { T3, readConfig } from '../adapter/t3.mjs';

const descriptor = { environmentId:'env-a',serverVersion:'0.0.0-fixture',orchestrationProtocolVersion:2,capabilities:{serverResolvedCommandContext:true} };
const row = (id, text='Hello', kind='assistant_message') => ({sourceThreadId:'thread-a',sourceItemId:id,visibility:'local',item:{id,threadId:'thread-a',type:kind,text,messageId:`message-${id}`,streaming:false}});
function snapshot(rows=[row('item-a')]) {
  return {snapshotSequence:10,historyCursor:'opaque-history',hasMoreHistory:true,projection:{thread:{id:'thread-a',title:'Fixture',projectId:'project-a',modelSelection:{instanceId:'provider-a',model:'fixture-model'},runtimeMode:'approval-required',interactionMode:'default',worktreePath:null},
    visibleTurnItems:rows,turnItems:rows.map(r=>r.item),runtimeRequests:[],runs:[{id:'run-a',ordinal:1,status:'running'}]}};
}
const action = {id:'operation-a',environmentId:'env-a',threadId:'thread-a'};
function approval(s) {
  const item = {id:'approval-item',type:'approval_request',requestId:'request-a',prompt:'Run the fixture command?',options:[{decision:'accept',label:'Once'},{decision:'decline',label:'Decline'}]};
  s.projection.turnItems.push(item); s.projection.runtimeRequests.push({id:'request-a',status:'pending',responseCapability:{type:'live',providerSessionId:'p'}}); return s;
}

test('UTF-8 clipping never splits a scalar',()=>{
  for(const value of ['aé日🙂z','🙂'.repeat(100)]) for(let n=0;n<25;n++) {
    const c=clip(value,n); assert.ok(Buffer.byteLength(c.text)<=n); assert.ok(!c.text.includes('\ufffd'));
  }
});
test('view bounds text bytes AND row count, including live growth',()=>{
  const v=view(snapshot(Array.from({length:250},(_,i)=>row(`i${i}`,'x'.repeat(15000)))),'thread-a');
  assert.ok(Buffer.byteLength(JSON.stringify(v.items))<LIMIT.view+100); assert.ok(v.limited);
  v.items=Array.from({length:100},(_,i)=>({...boundedRows([row(`i${i}`)])[0],text:'x'.repeat(LIMIT.text)}));
  trimView(v); assert.ok(Buffer.byteLength(JSON.stringify(v.items))<LIMIT.view+100);
});
test('host identity and wire version are enforced',()=>{
  assert.throws(()=>validateDescriptor(descriptor,'env-b'),{code:'identity_mismatch'});
  assert.throws(()=>validateDescriptor({...descriptor,orchestrationProtocolVersion:3},'env-a'),{code:'incompatible'});
  assert.equal(validateDescriptor(descriptor,'env-a'),descriptor);
});
test('send uses human path and server delivery selection',()=>{
  const p=snapshot().projection;
  const c=command('send',{...action,message:'Hello',mode:'auto'},p,descriptor);
  assert.equal(c.createdBy,'user'); assert.equal(c.creationSource,'web'); assert.equal(c.deliveryIntent,'auto');
  assert.equal(c.messageId,'operation-a:message');
  const q=command('send',{...action,message:'Next',mode:'queue'},p,descriptor);
  assert.deepEqual(q.dispatchMode,{type:'queue_after_active'}); assert.equal(q.deliveryIntent,undefined);
  assert.throws(()=>command('send',{...action,message:'x'},p,{capabilities:{}}),{code:'incompatible'});
});
test('Stop targets reviewed active run and holds queue',()=>{
  const p=snapshot().projection;
  assert.deepEqual(command('stop',{...action,runId:'run-a'},p,descriptor),{type:'run.interrupt',commandId:action.id,threadId:'thread-a',runId:'run-a',holdQueue:true});
  assert.throws(()=>command('stop',{...action,runId:'other'},p,descriptor),{code:'stale'});
});
test('approval requires current complete request and offered decision',()=>{
  const p=approval(snapshot()).projection, r=requests(p)[0];
  assert.equal(command('answer',{...action,requestId:r.id,revision:r.revision,decision:'accept'},p,descriptor).decision,'accept');
  assert.throws(()=>command('answer',{...action,requestId:r.id,revision:'old',decision:'accept'},p,descriptor),{code:'stale'});
  assert.throws(()=>command('answer',{...action,requestId:r.id,revision:r.revision,decision:'acceptAlways'},p,descriptor));
  p.turnItems.at(-1).prompt='x'.repeat(20000); assert.equal(requests(p)[0].supported,false);
});
test('unknown or nonresumable request cannot become an approval',()=>{
  const p=approval(snapshot()).projection;
  p.runtimeRequests[0].responseCapability={type:'not_resumable'};
  assert.equal(requests(p)[0].supported,false);
  p.runtimeRequests[0].responseCapability={type:'unknown'}; assert.equal(requests(p)[0].supported,false);
});
test('new thread preserves selected project and workspace without copying history',()=>{
  const p=snapshot().projection; p.thread.worktreePath='/tmp/fixture-worktree';p.thread.branch='fixture';
  const c=launch({...action,title:'New',message:'Task'},p);
  assert.equal(c.projectId,'project-a'); assert.equal(c.workspaceStrategy.type,'existing_worktree');
  assert.equal(c.initialMessage.text,'Task'); assert.equal(c.threadId,action.id);
});
test('view workspace is the exact launch binding or root; malformed paths are never clipped or relabelled',()=>{
  const s=snapshot();assert.equal(view(s,'thread-a').workspace,'root');
  s.projection.thread.worktreePath='/fixture/existing-worktree';
  const v=view(s,'thread-a');assert.equal(v.workspace,launch({...action,title:'New',message:'Task'},s.projection).workspaceStrategy.worktreePath);
  assert.equal(v.projectId,'project-a');
  for(const path of ['x'.repeat(4097),'漢'.repeat(1366),'bad\u001bpath','',{}]) {
    s.projection.thread.worktreePath=path;assert.equal(Object.hasOwn(view(s,'thread-a'),'workspace'),false);
  }
});
test('probe advertises launch selection additively with the native writable command capability',async()=>{
  const server=new T3({environmentId:'env-a',allowWrites:true},()=>{});
  try {
    server.http=async()=>structuredClone(descriptor);
    let hello=await server.probe();assert.equal(hello.protocol,1);assert.equal(hello.features.launchModelSelection,true);assert.equal(hello.writesEnabled,true);
    server.http=async()=>({...descriptor,capabilities:{}});
    hello=await server.probe();assert.equal(hello.features.launchModelSelection,false);assert.equal(hello.writesEnabled,false);
  } finally {server.close();}
});
test('canonical fingerprints tolerate JSON key order, not changed content',()=>{
  assert.equal(hash({x:1,y:{b:2,a:3}}),hash({y:{a:3,b:2},x:1}));
  assert.notEqual(hash({x:1}),hash({x:2}));
});
class Socket extends EventTarget {
  readyState=1; sent=[];
  send(s){this.sent.push(JSON.parse(s));}
  close(){this.readyState=3;this.dispatchEvent(new Event('close'));}
  incoming(data){this.dispatchEvent(new MessageEvent('message',{data:JSON.stringify(data)}));}
}
test('RPC correlates requests, acknowledges chunks, cancels owned stream',async()=>{
  const s=new Socket(), rpc=new Rpc(s), values=[];
  const stream=rpc.call('orchestration.subscribeThread',{threadId:'thread-a'},v=>values.push(v));
  s.incoming({_tag:'Chunk',requestId:'1',values:[{kind:'synchronized'}]});
  assert.equal(values.length,1); assert.deepEqual(s.sent.at(-1),{_tag:'Ack',requestId:'1'});
  stream.cancel(); await stream.promise;
  const call=rpc.call('orchestration.dispatchCommand',{type:'fixture'});
  s.incoming({_tag:'Exit',requestId:'2',exit:{_tag:'Success',value:{sequence:11}}});
  assert.deepEqual(await call.promise,{sequence:11});rpc.close();
});
test('socket loss marks unacknowledged operation uncertain',async()=>{
  const s=new Socket(),rpc=new Rpc(s);const call=rpc.call('fixture',{});s.close();
  await assert.rejects(call.promise,e=>e.uncertain===true);
});
test('host token and config must be private, not symlinks',async()=>{
  const dir=await mkdtemp(join(tmpdir(),'t3oxide-'));
  try {
    const token=join(dir,'token'), config=join(dir,'host.json');
    await writeFile(token,'fixture-token',{mode:0o600});
    await writeFile(config,JSON.stringify({baseUrl:'http://127.0.0.1:3773',environmentId:'env-a',tokenFile:token}),{mode:0o600});
    assert.equal((await readConfig(config)).environmentId,'env-a');
    await chmod(token,0o644);await assert.rejects(readConfig(config),{code:'configuration'});await chmod(token,0o600);
    const link=join(dir,'link'); await symlink(config,link);await assert.rejects(readConfig(link),{code:'configuration'});
  } finally {await rm(dir,{recursive:true,force:true});}
});
function backend() {
  let data=snapshot(), calls=[];
  const server=new T3({baseUrl:'http://127.0.0.1:3773',environmentId:'env-a',token:'fixture-token',allowWrites:true},()=>{});
  server.connect=async()=>{server.descriptor=descriptor;};
  server.bounded=async()=>structuredClone(data);
  server.rpc={call(tag,payload){calls.push({tag,payload});return{promise:Promise.resolve(tag==='orchestration.launchThread'?{threadId:payload.threadId}:{sequence:12}),cancel(){}};},close(){}};
  return{server,calls,set:(next)=>{data=next;}};
}
test('accepted same-ID action is not duplicated; changed content is rejected',async()=>{
  const {server,calls}=backend();
  const a={...action,message:'Task',mode:'auto'};
  await server.act('send',a);await server.act('send',{mode:'auto',message:'Task',...action});
  assert.equal(calls.length,1);
  await assert.rejects(server.act('send',{...a,message:'Different'}));server.close();
});
test('history replaces the bounded page, stops live stream, never mutates server',async()=>{
  const {server,calls}=backend(); server.watchId='thread-a';server.current=view(snapshot(),'thread-a');
  let cancelled=0;server.subscription={cancel(){cancelled++;}};
  server.http=async()=>({items:[row('earlier','Older text')],nextCursor:null,hasMoreHistory:false});
  await server.history('opaque-history');
  assert.equal(cancelled,1);assert.equal(server.current.historical,true);assert.equal(server.current.items.length,1);
  assert.equal(server.following,false);assert.equal(calls.length,0);server.close();
});
test('streaming updates are coalesced and bounded without full projection retention',async()=>{
  const {server}=backend();server.watchId='thread-a';server.current=view(snapshot(),'thread-a');server.snapshotSequence=10;
  for(let i=11;i<200;i++) server.onEvent({kind:'event',sequence:i,event:{threadId:'thread-a',type:'turn-item.updated',payload:{id:'item-a',type:'assistant_message',text:'🙂'.repeat(20000),streaming:true}}});
  assert.ok(server.emitTimer); assert.ok(Buffer.byteLength(server.current.items[0].text)<=LIMIT.text);
  assert.equal(server.current.projection,undefined);server.close();
});
test('HTTP response budget is enforced before JSON decoding; redirects refused',async()=>{
  let options;
  const server=new T3({baseUrl:'http://127.0.0.1:1',token:'fixture',environmentId:'env-a'},()=>{}, {fetch:async(_url,o)=>{
    options=o;return new Response(new ReadableStream({start(c){c.enqueue(new Uint8Array(LIMIT.upstream+1));c.close();}}));
  }});
  await assert.rejects(server.http('/fixture'),{code:'too_large'});assert.equal(options.redirect,'error');server.close();
});

test('readable rows preserve native run identity for causal send receipts', () => {
  const native = row('causal-output');
  native.item.runId = 'run-new';
  const projected = boundedRows([native])[0];
  assert.equal(projected.runId, 'run-new');
  for (const invalid of [null, '', 'bad\nrun', 7]) {
    native.item.runId = invalid;
    assert.equal(boundedRows([native])[0].runId, undefined);
  }
});
