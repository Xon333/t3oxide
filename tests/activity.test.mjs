import test from 'node:test';
import assert from 'node:assert/strict';
import { ACTIVITY_LIMIT, activityDetail, activityItem, activityPage, outputText, toolArgument, trimActivity } from '../adapter/activity.mjs';
import { T3 } from '../adapter/t3.mjs';
import { modelView } from '../adapter/models.mjs';
import { LIMIT } from '../adapter/core.mjs';

const tool = (id = 'tool', extra = {}) => ({sourceThreadId:'thread', sourceItemId:id, position:1,
  item:{id, threadId:'thread', runId:null, type:'command_execution', title:'Run tests', input:'go test ./...',
    status:'running', outputOmitted:true, updatedAt:'2026-10-24T10:00:00Z', ...extra}});
const snapshot = rows => ({snapshotSequence:10, historyCursor:'opaque', hasMoreHistory:true,
  projection:{thread:{id:'thread', title:'Fixture', projectId:'project', modelSelection:{instanceId:'provider',model:'model'}},
    runs:[], runtimeRequests:[], turnItems:[], visibleTurnItems:rows}});
function backend(rows = [tool()]) {
  const calls = [];
  const server = new T3({environmentId:'env',baseUrl:'http://127.0.0.1:1',allowWrites:false},()=>{});
  server.current = {...modelView(snapshot(rows),'thread'), state:'live',live:true,historical:false};
  server.watchId='thread'; server.following=true;
  server.connect=async()=>{};
  server.rpc={call(tag,payload){calls.push({tag,payload});return {promise:Promise.resolve({item:tool().item}),cancel(){}};},close(){}};
  return {server,calls};
}

test('ported t3term argument and nested output extraction retain structured tool meaning',()=>{
  assert.equal(toolArgument({input:{ignored:'x',file_path:'/repo/file.go',command:'wrong priority'}}),'/repo/file.go');
  assert.equal(toolArgument({input:'first\nsecond'}),'first');
  assert.equal(outputText({file:{content:'package main'}}),'package main');
  assert.equal(outputText([{type:'text',text:'first'},{type:'text',text:'second'}]),'first\nsecond');
  assert.equal(outputText({success:true}),'{"success":true}');
});

test('activity never consumes message slots, changes readable history or fetches output',()=>{
  const readable=Array.from({length:30},(_,i)=>({sourceThreadId:'thread',sourceItemId:`message-${i}`,
    item:{id:`message-${i}`,threadId:'thread',type:'assistant_message',text:'message'}}));
  const without=modelView(snapshot(readable),'thread');
  const withTools=modelView(snapshot([...readable,...Array.from({length:100},(_,i)=>tool(`tool-${i}`))]),'thread');
  assert.deepEqual(withTools.items,without.items);
  assert.equal(withTools.historyCursor,without.historyCursor);
  assert.equal(withTools.activity.items.length,24);
  assert.equal(withTools.activity.total,100);assert.equal(withTools.activity.limited,true);
  assert.ok(Buffer.byteLength(JSON.stringify(withTools.activity.items))<=ACTIVITY_LIMIT.page);
  assert.equal(withTools.activity.items[0].itemId,'tool-76');
  assert.equal(JSON.parse(Buffer.from(withTools.activity.historyCursor,'base64url')).si,'tool-76');
  assert.equal(withTools.activity.items[0].runId,'');
});

test('activity IDs retain inherited source identity and an oversized identity fails explicitly',()=>{
  const inherited=tool();inherited.sourceThreadId='parent';inherited.item.threadId='parent';
  const item=activityItem(inherited);assert.equal(item.key,'parent/tool');
  const huge=tool('"'.repeat(4096));huge.sourceThreadId='"'.repeat(4096);huge.item.threadId=huge.sourceThreadId;
  assert.throws(()=>activityPage([huge]),{code:'too_large'});
});

test('streaming activity summaries retain bounded metadata',()=>{
  const page=activityPage(Array.from({length:24},(_,i)=>tool(`tool-${i}`,{input:'x'.repeat(768),title:'a'.repeat(256)})),{sequence:10});
  page.items=Array.from({length:24},(_,i)=>activityItem(tool(`tool-${i}`,{input:'y'.repeat(768),title:'b'.repeat(256)})));
  trimActivity(page,11);
  assert.ok(Buffer.byteLength(JSON.stringify(page.items))<=ACTIVITY_LIMIT.page);
  assert.ok(page.hasMore);assert.ok(page.historyCursor);
});

test('activity details are source-bound, size-bounded and revision-honest',()=>{
  const item=tool('tool',{output:'🙂'.repeat(30000),outputOmitted:false,updatedAt:'new'}).item;
  const detail=activityDetail(item,{threadId:'thread',itemId:'tool',revision:'old'});
  assert.equal(detail.changed,true);assert.equal(detail.requestedRevision,'old');assert.equal(detail.updatedAt,'new');
  assert.ok(detail.truncated);assert.ok(Buffer.byteLength(detail.output)<=ACTIVITY_LIMIT.detail);
  assert.ok(!detail.output.includes('\ufffd'));assert.ok(detail.outputPresent);
  assert.throws(()=>activityDetail({...item,threadId:'other'},{threadId:'thread',itemId:'tool'}),{code:'identity_mismatch'});
  assert.throws(()=>activityDetail({...item,id:'other'},{threadId:'thread',itemId:'tool'}),{code:'identity_mismatch'});
});

test('escaped detail payloads fit the encoded frame budget without rewriting identities',()=>{
  const detail=activityDetail(tool('tool',{input:'\u0000'.repeat(24000),output:'\u0001'.repeat(24000)}).item,
    {threadId:'thread',itemId:'tool'});
  assert.equal(detail.key,'thread/tool');assert.equal(detail.truncated,true);
  assert.ok(Buffer.byteLength(JSON.stringify(detail))<=ACTIVITY_LIMIT.detailFrame);
});

test('file changes and tasks do not claim absent patches or hide truncation',()=>{
  const change=activityDetail(tool('tool',{type:'file_change',fileName:'/repo/new',changes:[{operation:'move',oldPath:'/repo/old',path:'/repo/new'}],input:undefined}).item,{threadId:'thread',itemId:'tool'});
  assert.match(change.output,/move \/repo\/old → \/repo\/new/);assert.equal(change.truncated,false);
  for(const field of ['items','steps','todos']) {
    const tasks=activityDetail(tool('tool',{type:'todo_list',[field]:Array.from({length:25},()=>({text:'Task',status:'pending'}))}).item,{threadId:'thread',itemId:'tool'});
    assert.ok(tasks.truncated,field);
  }
  const literal=activityDetail(tool('tool',{output:'line\n… valid content'}).item,{threadId:'thread',itemId:'tool'});
  assert.equal(literal.truncated,false);
});

test('activity reads use HTTP only, keep existing attachment and use activity history scope',async()=>{
  const {server,calls}=backend(); const paths=[];const current=server.current;
  server.descriptor={environmentId:'env'};
  server.bounded=async id=>{assert.equal(id,'thread');return snapshot([tool()]);};
  server.http=async path=>{paths.push(path);return{snapshotSequence:10,items:[tool('older')],nextCursor:null,hasMoreHistory:false};};
  try {
    const first=await server.activity('thread');assert.equal(first.activity.items[0].itemId,'tool');
    const earlier=await server.activity('thread','opaque');assert.equal(earlier.activity.items[0].itemId,'older');
    assert.match(paths[0],/view=activity/);assert.equal(calls.length,0);assert.equal(server.current,current);
  } finally{server.close();}
});

test('single item inspection is read-only and does not start a thread subscription',async()=>{
  const {server,calls}=backend();
  try {
    const result=await server.inspectActivity({threadId:'thread',itemId:'tool',revision:'2026-10-24T10:00:00Z'});
    assert.equal(result.itemId,'tool');assert.equal(calls.length,1);assert.equal(calls[0].tag,'orchestration.getTurnItem');
    assert.equal(server.receipts.size,0);assert.equal(server.journal,null);
  }finally{server.close();}
});

test('UI inspector rejects invented source and revision before reading',async()=>{
  const {server,calls}=backend();
  try {
    await assert.rejects(server.inspectActivity({viewThreadId:'thread',threadId:'other',itemId:'tool',revision:'2026-10-24T10:00:00Z'}),{code:'stale'});
    await assert.rejects(server.inspectActivity({viewThreadId:'thread',threadId:'thread',itemId:'tool',revision:'wrong'}),{code:'stale'});
    assert.equal(calls.length,0);
  }finally{server.close();}
});

test('navigation cancels an outstanding UI detail read without producing uncertain write state',async()=>{
  const {server}=backend();let resolve,cancelled=0;
  server.rpc.call=()=>({promise:new Promise(r=>resolve=r),cancel(){cancelled++;resolve(null);}});
  try {
    const read=server.inspectActivity({viewThreadId:'thread',threadId:'thread',itemId:'tool',revision:'2026-10-24T10:00:00Z'});
    await new Promise(r=>setImmediate(r));server.cancelNavigation();
    await assert.rejects(read,e=>e.code==='cancelled'&&e.uncertain===false);
    assert.equal(cancelled,1);assert.equal(server.receipts.size,0);
  }finally{server.close();}
});

test('unavailable or malformed detail reads remain explicit and never become cached success',async()=>{
  const {server}=backend();
  try{
    server.rpc.call=()=>({promise:Promise.resolve({item:null}),cancel(){}});
    await assert.rejects(server.inspectActivity({threadId:'thread',itemId:'tool'}),{code:'unavailable'});
    server.rpc.call=()=>({promise:Promise.resolve({}),cancel(){}});
    await assert.rejects(server.inspectActivity({threadId:'thread',itemId:'tool'}),{code:'unsupported'});
  }finally{server.close();}
});

test('known tool status updates patch bounded metadata without another snapshot read',()=>{
  const {server}=backend();let refreshes=0;server.scheduleRefresh=()=>refreshes++;
  try{
    server.onEvent({kind:'event',sequence:11,event:{type:'turn-item.updated',threadId:'thread',payload:tool('tool',{status:'completed',updatedAt:'new'}).item}});
    assert.equal(server.current.activity.items[0].status,'completed');assert.equal(refreshes,0);
    server.onEvent({kind:'event',sequence:12,event:{type:'turn-item.updated',threadId:'thread',payload:tool('tool',{input:'x'.repeat(2000)}).item}});
    server.flushView();
    assert.equal(server.current.activity.limited,true);assert.equal(server.current.activity.items[0].truncated,true);
    server.onEvent({kind:'event',sequence:13,event:{type:'turn-item.updated',threadId:'thread',payload:tool('new').item}});
    assert.equal(refreshes,1);
  }finally{server.close();}
});

test('optional activity never makes a previously fitting transcript and exact requests overflow',()=>{
  const requests=Array.from({length:8},(_,i)=>({id:'r'+i+'x'.repeat(4000),status:'pending',responseCapability:{type:'live'}}));
  const req=requests.map((r,i)=>({id:'approval'+i,threadId:'t',type:'approval_request',requestId:r.id,
    prompt:'p'.repeat(12288),options:[{decision:'accept',label:'o'.repeat(12150)}]}));
  const tools=Array.from({length:24},(_,i)=>({id:'tool'+i,threadId:'t',type:'dynamic_tool',title:'T'.repeat(250),input:'x'.repeat(700)}));
  const rows=[...tools,...req,{id:'message',threadId:'t',type:'assistant_message',text:'\u0000'.repeat(24000)}]
    .map((item,i)=>({position:i,sourceThreadId:'t',sourceItemId:item.id,item}));
  const s={snapshotSequence:1,projection:{thread:{id:'t',title:'Fixture'},runs:[],runtimeRequests:requests,visibleTurnItems:rows,turnItems:req}};
  const packets=[];const server=new T3({environmentId:'e'},(event,data)=>packets.push({v:1,environmentId:'e',event,data}));
  try {
    server.current={...modelView(s,'t'),state:'live',live:true,historical:false};
    const before=structuredClone(server.current);server.flushView();
    assert.ok(Buffer.byteLength(JSON.stringify(packets[0]))+1<=LIMIT.frame);
    assert.deepEqual(packets[0].data.items,before.items);assert.deepEqual(packets[0].data.requests,before.requests);
    assert.ok(!packets[0].data.activity||packets[0].data.activity.limited);
    assert.equal(server.current.activity.items.length,before.activity.items.length,'frame fitting must not discard the explicitly readable tool window');
    const original=packets[0].data.revision;
    server.current.items[0].text+='x';server.flushView();
    assert.ok(Buffer.byteLength(JSON.stringify(packets.at(-1)))+1<=LIMIT.frame);
    assert.equal(packets.at(-1).data.revision,original+1);
    // A valid previous 48 KiB page can carry long inherited identities. Their
    // removal keys plus the new 374 KiB view no longer fit as a delta.
    const largeRows=Array.from({length:3},(_,i)=>{
      const source='s'.repeat(3900),id=String(i)+'i'.repeat(3899);
      return {key:source+'/'+id,sourceThreadId:source,itemId:id,messageId:null,kind:'assistant_message',
        text:'old',truncated:false,streaming:false,attachmentCount:0,position:i};
    });
    server.current.items=largeRows;server.flushView();
    server.current.items=before.items;server.flushView();
    assert.equal(packets.at(-1).event,'view','large removal keys should fall back to a complete bounded replacement');
    assert.deepEqual(packets.at(-1).data.items,before.items);
    assert.ok(Buffer.byteLength(JSON.stringify(packets.at(-1)))+1<=LIMIT.frame);
  }finally{server.close();}
});
