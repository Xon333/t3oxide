import test from 'node:test';
import assert from 'node:assert/strict';
import { setImmediate as tick } from 'node:timers/promises';
import { T3 } from '../adapter/t3.mjs';
import { LIMIT, view, historyAnchor, identifier } from '../adapter/core.mjs';

const row = (i, text = `Row ${i}`) => ({ position:i, sourceThreadId:'thread-a', sourceItemId:`i${i}`,
  item:{id:`i${i}`,threadId:'thread-a',type:'assistant_message',messageId:`m${i}`,text} });
const snapshot = (rows, sequence=10) => ({ snapshotSequence:sequence, historyCursor:null, hasMoreHistory:false,
  projection:{ thread:{id:'thread-a',title:'Fixture'}, runs:[], runtimeRequests:[], visibleTurnItems:rows } });
test('native nested request identities remain intact beyond 256 bytes',()=>{
  const id='turn-item:runtime-request:'+('thread%253Adelegated-task%253A'.repeat(12));
  const source={...row(0),sourceItemId:id,item:{...row(0).item,id}};
  const result=view(snapshot([source]),'thread-a');
  assert.equal(result.items[0].itemId,id);
  assert.equal(JSON.parse(Buffer.from(historyAnchor(result.items[0],10),'base64url')).si,id);
  assert.throws(()=>identifier('x'.repeat(4097)),{code:'invalid_request'});
});
function backend(rows = [row(0)]) {
  const emitted = [], subscriptions = [];
  const server = new T3({baseUrl:'http://127.0.0.1:3773',environmentId:'env-a',token:'fixture'}, (event,data)=>emitted.push({event,data}));
  const rpc = { call(_tag,_args,callback) {
    let resolve, reject;
    const promise = new Promise((a,b)=>{resolve=a;reject=b;});
    const sub = {promise,callback,reject,cancel(){resolve(null);}};
    subscriptions.push(sub); return sub;
  }, close(){} };
  server.connect = async()=>{server.rpc=rpc;};
  server.bounded = async()=>snapshot(rows);
  return {server,emitted,subscriptions};
}
function update(server, i, sequence, text) {
  server.onEvent({kind:'event',sequence,event:{type:'message.updated',threadId:'thread-a',payload:{id:`m${i}`,text,streaming:true}}});
}
// This fixture follows the pinned upstream's exclusive identity-first cursor.
// It deliberately returns large complete-turn pages, as the real endpoint can.
function historySource(server, rows) {
  server.http = async path => {
    const cursor = JSON.parse(Buffer.from(new URL(path,'http://fixture').searchParams.get('cursor'),'base64url'));
    const at = rows.findIndex(r=>r.sourceThreadId===cursor.st && r.sourceItemId===cursor.si);
    assert.ok(at>=0, 'anchor must reference a row retained by the source');
    return {snapshotSequence:10,items:rows.slice(0,at),nextCursor:null,hasMoreHistory:false};
  };
}
async function collectPages(server) {
  const seen=[]; let pages=0;
  do {
    assert.ok(++pages<150,'paging must make progress');
    assert.ok(server.current.items.length<=LIMIT.items);
    assert.ok(Buffer.byteLength(JSON.stringify(server.current.items))<=LIMIT.view);
    seen.unshift(...server.current.items.map(r=>r.itemId));
    if (!server.current.hasMore) break;
    assert.ok(server.current.historyCursor);
    await server.history(server.current.historyCursor);
  } while (true);
  return seen;
}
test('one update among 60 rows emits one upsert and reconstructs the exact view',async()=>{
  const {server,emitted,subscriptions}=backend(Array.from({length:60},(_,i)=>row(i)));
  try {
    await server.watch('thread-a'); server.flushView();
    assert.equal(emitted.at(-1).event,'view'); assert.equal(emitted.at(-1).data.state,'synchronizing');
    const initial=emitted.at(-1).data;
    subscriptions[0].callback({kind:'synchronized'});server.flushView();
    assert.equal(emitted.at(-1).event,'delta'); assert.equal(emitted.at(-1).data.upserts.length,0);
    update(server,30,11,'Only this row changed');server.flushView();
    const delta=emitted.at(-1).data;
    assert.equal(emitted.at(-1).event,'delta'); assert.equal(delta.upserts.length,1);
    assert.equal(delta.upserts[0].itemId,'i30'); assert.equal(delta.baseRevision,initial.revision+1);
    assert.equal(delta.revision,delta.baseRevision+1); assert.equal(delta.metadata.state,'live');
    const map=new Map(initial.items.map(r=>[r.key,r]));
    delta.removed.forEach(k=>map.delete(k));delta.upserts.forEach(r=>map.set(r.key,r));
    assert.deepEqual(delta.order.map(k=>map.get(k)),server.current.items);
  } finally {server.close();}
});
test('100 readable rows remain reachable after the 80-row initial trim',async()=>{
  const rows=Array.from({length:100},(_,i)=>row(i));const {server}=backend(rows);
  try {await server.watch('thread-a');historySource(server,rows);
    assert.equal(server.current.items.length,80);assert.equal(server.current.hasMore,true);
    assert.deepEqual(await collectPages(server),rows.map(r=>r.sourceItemId));
  } finally {server.close();}
});
test('byte trimming on initial and history pages leaves every readable row reachable',async()=>{
  const rows=Array.from({length:100},(_,i)=>row(i,'x'.repeat(20000)));const {server}=backend(rows);
  try {await server.watch('thread-a');historySource(server,rows);
    assert.ok(server.current.items.length<80);
    assert.deepEqual(await collectPages(server),rows.map(r=>r.sourceItemId));
  } finally {server.close();}
});
test('streaming growth removes bounded rows with a continuation before the retained anchor',async()=>{
  const rows=Array.from({length:60},(_,i)=>row(i));const {server,emitted}=backend(rows);
  try {await server.watch('thread-a');server.flushView();
    for(let i=0;i<60;i++) update(server,i,11+i,'x'.repeat(20000));
    server.flushView();const delta=emitted.at(-1).data;
    assert.ok(delta.removed.length>0);assert.equal(delta.metadata.hasMore,true);
    assert.equal(delta.metadata.historyCursor,historyAnchor(server.current.items[0],server.current.sequence));
    historySource(server,rows);
    assert.deepEqual(await collectPages(server),rows.map(r=>r.sourceItemId));
  } finally {server.close();}
});
test('failed history is unavailable and historical; reconnect does not jump to Latest',async()=>{
  const rows=Array.from({length:100},(_,i)=>row(i));const {server,subscriptions,emitted}=backend(rows);
  try {await server.watch('thread-a');subscriptions[0].callback({kind:'synchronized'});server.flushView();
    server.http=async()=>{throw new Error('fixture HTTP failure');};
    await assert.rejects(server.history(server.current.historyCursor));server.flushView();
    assert.equal(server.current.state,'unavailable');assert.equal(server.current.live,false);
    assert.equal(server.current.historical,true);assert.equal(server.following,false);
    const keys=server.current.items.map(r=>r.key);
    server.wantConnection=true;server.retryDelay=0;
    server.disconnected();await new Promise(r=>setTimeout(r,10));server.flushView();
    assert.equal(server.current.state,'historical');assert.equal(server.current.historical,true);
    assert.equal(server.current.live,false);assert.equal(subscriptions.length,1);
    assert.equal(emitted.at(-1).event,'view');assert.deepEqual(server.current.items.map(r=>r.key),keys);
  } finally {server.close();}
});
test('Latest failure preserves historical intent and subscription failure emits unavailable',async()=>{
  const rows=Array.from({length:100},(_,i)=>row(i));const {server,subscriptions}=backend(rows);
  try {await server.watch('thread-a');historySource(server,rows);await server.history(server.current.historyCursor);
    server.bounded=async()=>{throw new Error('fixture');};
    await assert.rejects(server.watch('thread-a'));server.flushView();
    assert.equal(server.current.historical,true);assert.equal(server.current.state,'unavailable');assert.equal(server.following,false);
    server.bounded=async()=>snapshot(rows);await server.watch('thread-a');
    subscriptions.at(-1).reject(new Error('subscription failed'));await tick();server.flushView();
    assert.equal(server.current.state,'unavailable');assert.equal(server.current.live,false);
  } finally {server.close();}
});
test('cancelNavigation aborts the in-flight HTTP body but not independent control reads',async()=>{
  const {server}=backend();const reads=[];
  server.fetch=async(_url,{signal})=>{
    const entry={signal}; reads.push(entry);
    return new Response(new ReadableStream({start(controller){
      entry.finish=()=>{controller.enqueue(Buffer.from(JSON.stringify(snapshot([row(0)]))));controller.close();};
      signal.addEventListener('abort',()=>controller.error(new DOMException('Aborted','AbortError')),{once:true});
    }}));
  };
  delete server.bounded; // exercise the real HTTP read and its AbortSignal
  try {
    const navigation=server.watch('thread-a');await tick();
    const control=server.bounded('thread-a');await tick();
    server.cancelNavigation();
    await assert.rejects(navigation,{code:'cancelled'});
    assert.equal(reads[0].signal.aborted,true);assert.equal(reads[1].signal.aborted,false);
    reads[1].finish();await control;assert.equal(server.current,null);
  } finally {server.close();}
});
test('superseded history completion cannot overwrite a newer watched thread',async()=>{
  const rows=Array.from({length:100},(_,i)=>row(i));const {server}=backend(rows);
  try {await server.watch('thread-a');let resolve;
    server.http=()=>new Promise(r=>{resolve=r;});
    const history=server.history(server.current.historyCursor);
    server.bounded=async()=>snapshot([row(500)],20);await server.watch('thread-a');
    resolve({snapshotSequence:10,items:[row(0)],nextCursor:null,hasMoreHistory:false});await history;
    assert.equal(server.current.items[0].itemId,'i500');assert.equal(server.current.historical,false);
  } finally {server.close();}
});
test('reconnect live view takes a new snapshot and waits for synchronization',async()=>{
  const {server,subscriptions,emitted}=backend();
  try {await server.watch('thread-a');subscriptions[0].callback({kind:'synchronized'});server.flushView();
    const revision=emitted.at(-1).data.revision;
    server.wantConnection=true;server.retryDelay=0;server.disconnected();server.flushView();
    assert.equal(server.current.state,'unavailable');assert.equal(server.current.live,false);
    await new Promise(r=>setTimeout(r,10));server.flushView();
    assert.equal(server.current.state,'synchronizing');assert.equal(emitted.at(-1).event,'view');
    assert.ok(emitted.at(-1).data.revision>revision);assert.equal(subscriptions.length,2);
    subscriptions[0].callback({kind:'synchronized'});assert.equal(server.current.live,false);
    subscriptions[1].callback({kind:'synchronized'});assert.equal(server.current.live,true);
  } finally {server.close();}
});
test('escaped text still admits a bounded readable row and continues paging',async()=>{
  const rows=Array.from({length:5},(_,i)=>row(i,'\u0000'.repeat(LIMIT.text)));const {server}=backend(rows);
  try {await server.watch('thread-a');historySource(server,rows);
    assert.ok(server.current.items.length>0);assert.ok(server.current.items.every(r=>r.truncated));
    assert.deepEqual(await collectPages(server),rows.map(r=>r.sourceItemId));
  } finally {server.close();}
});
test('delayed reconnect cannot restart a newer navigation',async()=>{
  const {server,subscriptions}=backend();
  try {await server.watch('thread-a');server.wantConnection=true;server.retryDelay=5;server.disconnected();
    await server.watch('thread-a');await new Promise(r=>setTimeout(r,15));
    assert.equal(subscriptions.length,2);assert.equal(server.current.state,'synchronizing');
  } finally {server.close();}
});
test('failed refresh emits unavailable and cannot leave live controls on screen',async()=>{
  const {server,subscriptions,emitted}=backend();
  try {await server.watch('thread-a');subscriptions[0].callback({kind:'synchronized'});server.flushView();
    server.bounded=async()=>{throw new Error('fixture');};await assert.rejects(server.refresh());server.flushView();
    assert.equal(emitted.at(-1).event,'delta');assert.equal(emitted.at(-1).data.metadata.state,'unavailable');
    assert.equal(emitted.at(-1).data.metadata.live,false);
  } finally {server.close();}
});
test('a newer stream reset cannot be overwritten by an older pending HTTP refresh',async()=>{
  const {server}=backend();
  try {await server.watch('thread-a');let resolve;server.bounded=()=>new Promise(r=>{resolve=r;});
    const refreshing=server.refresh();server.onEvent({kind:'snapshot',...snapshot([row(200)],30)});
    resolve(snapshot([row(100)],20));await refreshing;
    assert.equal(server.current.items[0].itemId,'i200');assert.equal(server.snapshotSequence,30);
  } finally {server.close();}
});
test('successful refresh recovers live only while the subscription is synchronized',async()=>{
  const {server,subscriptions}=backend();
  try {await server.watch('thread-a');subscriptions[0].callback({kind:'synchronized'});
    server.bounded=async()=>{throw new Error('fixture');};await assert.rejects(server.refresh());
    assert.equal(server.current.state,'unavailable');
    server.bounded=async()=>snapshot([row(0)],20);await server.refresh();assert.equal(server.current.state,'live');
    subscriptions[0].reject(new Error('lost subscription'));await tick();
    await server.refresh();assert.equal(server.current.live,false);assert.equal(server.current.state,'synchronizing');
  } finally {server.close();}
});
test('connection loss during Latest from history retains historical reconnect intent',async()=>{
  const rows=Array.from({length:100},(_,i)=>row(i));const {server}=backend(rows);
  try {await server.watch('thread-a');historySource(server,rows);await server.history(server.current.historyCursor);
    let resolve;server.bounded=()=>new Promise(r=>{resolve=r;});
    const latest=server.watch('thread-a');await tick();server.disconnected();
    resolve(snapshot(rows));await latest;
    assert.equal(server.following,false);assert.equal(server.current.historical,true);
    assert.equal(server.current.state,'unavailable');
  } finally {server.close();}
});
