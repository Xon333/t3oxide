import test from 'node:test';
import assert from 'node:assert/strict';
import { requests, view } from '../adapter/core.mjs';
import { Rpc } from '../adapter/wire.mjs';
import { T3 } from '../adapter/t3.mjs';
function snapshot() {
  const item={id:'item-a',threadId:'thread-a',type:'assistant_message',messageId:'message-item-a',text:'Hello'};
  return {snapshotSequence:10,projection:{thread:{id:'thread-a',title:'Fixture',projectId:'project-a'},runs:[],runtimeRequests:[],turnItems:[],visibleTurnItems:[{sourceThreadId:'thread-a',sourceItemId:'item-a',item}]}};
}
function approval(s) {
  s.projection.turnItems.push({id:'approval',requestId:'request-a',type:'approval_request',prompt:'Approve fixture?',options:[{decision:'accept',label:'Once'}]});
  s.projection.runtimeRequests.push({id:'request-a',status:'pending',responseCapability:{type:'live'}});
  return s;
}
function backend() {
  return {server:new T3({baseUrl:'http://127.0.0.1:3773',environmentId:'env-a',token:'fixture'},()=>{})};
}
class Socket extends EventTarget {
  readyState=1;
  send(){}
  close(){this.readyState=3;this.dispatchEvent(new Event('close'));}
  incoming(data){this.dispatchEvent(new MessageEvent('message',{data:JSON.stringify(data)}));}
}

test('unfamiliar approval without offered options cannot fabricate consent',()=>{
  const p=approval(snapshot()).projection;
  delete p.turnItems.at(-1).options; p.turnItems.at(-1).requestKind='future-permission';
  assert.equal(requests(p)[0].supported,false); assert.deepEqual(requests(p)[0].options,[]);
});
test('malformed channel data preserves uncertain write outcome',async()=>{
  const socket=new Socket(),rpc=new Rpc(socket),call=rpc.call('write',{});
  socket.dispatchEvent(new MessageEvent('message',{data:'not-json'}));
  await assert.rejects(call.promise,e=>e.uncertain===true);
});
test('non-domain failure is not a definite command rejection',async()=>{
  const socket=new Socket(),rpc=new Rpc(socket),call=rpc.call('write',{});
  socket.incoming({_tag:'Exit',requestId:'1',exit:{_tag:'Failure',cause:[{_tag:'Die',defect:'fixture'}]}});
  await assert.rejects(call.promise,e=>e.uncertain===true);rpc.close();
});
test('out-of-order or replayed message update cannot restore older text',()=>{
  const {server}=backend();server.watchId='thread-a';server.current=view(snapshot(),'thread-a');server.snapshotSequence=10;
  for (const [sequence,value] of [[15,'new'],[14,'old'],[15,'duplicate']]) server.onEvent({kind:'event',sequence,event:{threadId:'thread-a',type:'message.updated',payload:{id:'message-item-a',text:value,streaming:true}}});
  assert.equal(server.current.items[0].text,'new');server.close();
});
test('refresh preserves newer streaming text and schedules structural catch-up',async()=>{
  const {server}=backend();server.watchId='thread-a';server.current=view(snapshot(),'thread-a');server.snapshotSequence=10;
  let resolve;server.bounded=()=>new Promise(r=>{resolve=r;});
  const refreshing=server.refresh();
  server.onEvent({kind:'event',sequence:20,event:{threadId:'thread-a',type:'message.updated',payload:{id:'message-item-a',text:'newer than snapshot',streaming:true}}});
  server.onEvent({kind:'event',sequence:21,event:{threadId:'thread-a',type:'runtime-request.updated',payload:{id:'new-request'}}});
  resolve({...snapshot(),snapshotSequence:15});await refreshing;
  assert.equal(server.current.items[0].text,'newer than snapshot');assert.ok(server.refreshTimer);
  server.close();
});
