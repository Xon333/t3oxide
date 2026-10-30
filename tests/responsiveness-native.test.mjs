import test from 'node:test';
import assert from 'node:assert/strict';
import {setImmediate as turn} from 'node:timers/promises';
import {T3, SCHEDULING} from '../adapter/t3.mjs';
import {Fault, view} from '../adapter/core.mjs';
import {nativeDefaultsReport, DEFAULTS_WRITE_REASON} from '../adapter/native.mjs';
import {nativeWorking, threadList} from '../adapter/navigation.mjs';
import {modelView, providerIdentity} from '../adapter/models.mjs';

const stamp='2026-10-30T06:00:00.000Z';
const selected={instanceId:'instance-a',model:'model-a',options:[{id:'opaque',value:'keep'}]};
const descriptor={environmentId:'env-a',serverVersion:'fixture-only',orchestrationProtocolVersion:2,
  platform:{machine:'laptop'},capabilities:{serverResolvedCommandContext:true,threadPinning:true}};
function snapshot(id='thread-a',sequence=10,requestRevision='') {
  const request={id:'request-a',status:'pending',responseCapability:{type:'live'}};
  const offer={id:'approval-a',type:'approval_request',requestId:'request-a',prompt:'Synthetic permission '+requestRevision,
    options:[{decision:'accept',label:'Once'},{decision:'decline',label:'Decline'}]};
  return {snapshotSequence:sequence,projection:{thread:{id,title:'Synthetic',projectId:'project-a',
    modelSelection:selected,runtimeMode:'approval-required',interactionMode:'default',worktreePath:null},
    runs:[],runtimeRequests:requestRevision?[request]:[],turnItems:requestRevision?[offer]:[],
    visibleTurnItems:[{sourceThreadId:id,sourceItemId:'item-a',position:0,
      item:{id:'item-a',threadId:id,type:'assistant_message',messageId:'message-a',text:'Synthetic body'}}]}};
}
function opening(t) {
  let clock=0, failTicket=false, subscribed;
  const counts={descriptor:0,ticket:0,socket:0,bounded:0,dispatch:0}, frames=[];
  const config={environmentId:'env-a',baseUrl:'http://127.0.0.1:1234',token:'fixture',allowWrites:true};
  const server=new T3(config,(event,data)=>frames.push({event,data,at:clock}),{
    now:()=>clock,
    fetch:async (url)=>{
      if(url.pathname==='/.well-known/t3/environment') {counts.descriptor++;return Response.json({...descriptor,environmentId:config.environmentId});}
      if(url.pathname==='/api/auth/websocket-ticket') {counts.ticket++;return failTicket?new Response('',{status:401}):Response.json({ticket:'fixture'});}
      if(url.pathname.endsWith('/bounded')) {counts.bounded++;return Response.json(snapshot());}
      throw new Error('Unexpected read');
    },
    connectSocket:async()=>{counts.socket++;return {close(){},call(tag,args,callback){
      assert.equal(tag,'orchestration.subscribeThread');
      assert.deepEqual(args,{threadId:'thread-a',afterSequence:10,requestCompletionMarker:true,acceptBoundedSnapshot:true});
      subscribed=callback;return {promise:new Promise(()=>{}),cancel(){}};
    }}}});
  t.after(()=>server.close());
  return {server,config,counts,frames,time:value=>clock=value,fail:value=>failTicket=value,sync:()=>subscribed({kind:'synchronized'})};
}

test('initial verified hello/watch consumes one descriptor and publishes bounded then live without text delay',async t=>{
  const f=opening(t);
  const hello=await f.server.probe();
  assert.equal(hello.machineKind,'laptop');assert.equal(hello.features.nativeDefaultsWrite,false);
  await f.server.watch('thread-a');
  assert.deepEqual(f.counts,{descriptor:1,ticket:1,socket:1,bounded:1,dispatch:0});
  const first=f.frames.find(x=>x.event==='view');
  assert.equal(first.data.state,'synchronizing');assert.equal(first.data.live,false);assert.equal(first.at,0);
  assert.equal(f.server.emitTimer,null);assert.equal(f.server.probeReceipt,null);
  f.sync();assert.equal(f.frames.at(-1).data.metadata.state,'live');assert.equal(f.server.emitTimer,null);
});

for(const invalidation of ['expiry','credential','identity','url','write-setting','generation','clock-regression','failed-ticket','disconnect']) {
  test('one-use verification revalidates on '+invalidation,async t=>{
    const f=opening(t);await f.server.probe();
    if(invalidation==='expiry')f.time(SCHEDULING.probeReceiptMs+1);
    if(invalidation==='credential')f.config.token='rotated-fixture';
    if(invalidation==='identity')f.config.environmentId='env-b';
    if(invalidation==='url')f.config.baseUrl='http://127.0.0.1:5678';
    if(invalidation==='write-setting')f.config.allowWrites=false;
    if(invalidation==='generation')f.server.generation++;
    if(invalidation==='clock-regression')f.time(-1);
    if(invalidation==='failed-ticket') {
      f.fail(true);await assert.rejects(f.server.connect(),{code:'authorization'});
      assert.equal(f.server.probeReceipt,null);f.fail(false);
    }
    if(invalidation==='disconnect')f.server.disconnected();
    await f.server.connect();
    assert.equal(f.counts.descriptor,2);assert.equal(f.counts.dispatch,0);
    assert.equal(f.server.probeReceipt,null);
  });
}

for(const readMs of [0,30,100])test('first bounded snapshot publishes at readiness '+readMs+'ms, marker still owns authority',async t=>{
  t.mock.timers.enable({apis:['setTimeout','Date'],now:1000});
  const frames=[];let finish,callback;
  const server=new T3({environmentId:'env-a'},(event,data)=>frames.push({event,data,at:Date.now()}));
  t.after(()=>server.close());
  server.connect=async()=>{server.rpc={close(){},call(_tag,_args,cb){callback=cb;return {promise:new Promise(()=>{}),cancel(){}}}}};
  server.bounded=()=>new Promise(resolve=>finish=resolve);
  const watching=server.watch('thread-a');await turn();
  t.mock.timers.tick(readMs);assert.equal(frames.length,0);
  finish(snapshot());await watching;
  assert.equal(frames[0].at,1000+readMs);assert.equal(frames[0].data.live,false);
  t.mock.timers.tick(300);assert.equal(frames.length,1);
  callback({kind:'synchronized'});assert.equal(frames.at(-1).data.metadata.live,true);
});

test('request burst upgrades structural cadence to one urgent read and one follow-up, preserving newer text',async t=>{
  t.mock.timers.enable({apis:['setTimeout','Date'],now:1000});
  const frames=[],reads=[];let finish;
  const server=new T3({environmentId:'env-a'},(event,data)=>frames.push({event,data,at:Date.now()}),{now:()=>Date.now()});
  t.after(()=>server.close());
  server.current={...view(snapshot(),'thread-a'),live:true,state:'live',historical:false};
  server.watchId='thread-a';server.synchronized=true;server.snapshotSequence=10;server.lastStreamSequence=10;
  server.flushView();frames.length=0;
  server.bounded=()=>{reads.push(Date.now());return new Promise(resolve=>finish=resolve)};
  server.onEvent({kind:'event',sequence:11,event:{threadId:'thread-a',type:'run.updated',payload:{}}});
  for(let i=12;i<112;i++)server.onEvent({kind:'event',sequence:i,event:{threadId:'thread-a',type:'runtime-request.updated',payload:{id:'request-a'}}});
  t.mock.timers.tick(0);await turn();assert.deepEqual(reads,[1000]);
  for(let i=112;i<212;i++)server.onEvent({kind:'event',sequence:i,event:{threadId:'thread-a',type:'runtime-request.updated',payload:{id:'request-a'}}});
  server.onEvent({kind:'event',sequence:212,event:{threadId:'thread-a',type:'message.updated',payload:{id:'message-a',text:'Newest exact body',streaming:true}}});
  finish(snapshot('thread-a',111,'first'));await turn();
  assert.equal(server.current.items[0].text,'Newest exact body');
  assert.equal(frames.at(-1).data.metadata.requests[0].prompt,'Synthetic permission first');
  t.mock.timers.tick(0);await turn();assert.equal(reads.length,2);
  finish(snapshot('thread-a',212,'newest'));await turn();
  assert.equal(frames.at(-1).data.metadata.requests[0].prompt,'Synthetic permission newest');
  assert.equal(server.emitTimer,null);assert.equal(server.refreshTimer,null);
  t.mock.timers.tick(10000);await turn();assert.equal(reads.length,2);
});

test('one-shot preview retains attachment and has no socket, subscription, request or journal authority',async t=>{
  const server=new T3({environmentId:'env-a'},()=>{});t.after(()=>server.close());
  server.descriptor=descriptor;server.watchId='attached-a';server.current={...view(snapshot(),'thread-a'),state:'live',live:true};
  let reads=0;server.bounded=async id=>{reads++;return snapshot(id,10,'pending')};
  server.connect=async()=>{throw new Error('Preview must never connect a writable socket')};
  const before=structuredClone(server.current);
  const result=await server.previewThread({threadId:'thread-b'});
  assert.equal(reads,1);assert.equal(result.readOnly,true);assert.equal(result.available,true);
  assert.equal(result.view.threadId,'thread-b');assert.equal(result.view.live,false);assert.equal(result.view.activeRunId,null);
  assert.equal(result.view.revision,1);assert.deepEqual(result.view.requests,[]);
  assert.equal(server.watchId,'attached-a');assert.deepEqual(server.current,before);assert.equal(server.receipts.size,0);
  let finish;server.bounded=()=>new Promise(resolve=>finish=resolve);
  const pending=server.previewThread({threadId:'thread-b'});await turn();server.cancelPreview();finish(snapshot('thread-b'));
  await assert.rejects(pending,{code:'cancelled'});
});

test('projection serialization reuses exact immutable rows without losing controls, unknown fields, or numeric values',t=>{
  const frames=[];const server=new T3({environmentId:'env-a'},(event,data)=>frames.push({event,data}));t.after(()=>server.close());
  server.current={...view(snapshot(),'thread-a'),state:'live',live:true,historical:false,
    future:{number:9007199254740991,string:'9007199254740991',nested:[true,null,'x']}};
  server.flushView();const previous=structuredClone(frames[0]);
  server.current.items[0].text='Replacement';server.current.future.nested.push('later');server.flushView();
  assert.deepEqual(frames[0],previous);assert.equal(frames[1].data.upserts[0].text,'Replacement');
  assert.equal(frames[1].data.metadata.future.number,9007199254740991);
  assert.equal(frames[1].data.metadata.future.string,'9007199254740991');
  const count=frames.length;server.flushView();assert.equal(frames.length,count);
  server.current.status='failed';server.flushView();assert.deepEqual(frames.at(-1).data.upserts,[]);
  assert.equal(frames.at(-1).data.metadata.status,'failed');
  assert.equal(server.sentRowJSON.size,1);
});

test('native defaults preserve exact unavailable selection, per-field source and inherited value without issuing any write',()=>{
  const project={id:'project-a',title:'Project',workspaceRoot:'/fixture/project'};
  const config={settings:{defaultModelSelection:{instanceId:'instance-b',model:'model-b'},defaultRuntimeMode:'approval-required',
    defaultThreadEnvMode:'local',projectSettingsFolded:true,projectSettingsOverrides:{'project-a':{defaultModelSelection:selected,unknownSibling:{opaque:true}}}},
    providers:[{instanceId:'instance-a',driver:'codex',displayName:'A',enabled:false,installed:true,status:'disabled',auth:{label:'Native A'},checkedAt:stamp,models:[{slug:'model-a'}]},
      {instanceId:'instance-b',enabled:true,installed:true,status:'ready',models:[{slug:'model-b'}]}]};
  const before=structuredClone(config);const projectReport=nativeDefaultsReport({projects:[project]},config,'project-a','project');
  assert.equal(projectReport.writeSupported,false);assert.equal(projectReport.writeReason,DEFAULTS_WRITE_REASON);
  assert.equal(projectReport.modelAvailable,false);assert.deepEqual(projectReport.storedModelSelection,selected);
  assert.equal(projectReport.fields[0].source,'project');assert.equal(projectReport.fields[0].inheritedValue.instanceId,'instance-b');
  assert.equal(projectReport.fields[1].source,'environment');assert.equal(projectReport.providerIdentity.accountLabel,'Native A');
  assert.equal(projectReport.desktopLocal,'unreported');assert.deepEqual(config,before);
  const environment=nativeDefaultsReport({projects:[project]},config,'project-a','environment');
  assert.equal(environment.fields[0].source,'environment');assert.equal(environment.storedModelSelection.instanceId,'instance-b');
  assert.equal(environment.providerIdentity.accountLabel,'');assert.notEqual(environment.revision,projectReport.revision);
});

test('provider identity stays exact and unreported until an explicit native label is available',t=>{
  const plain=modelView(snapshot(),'thread-a');
  assert.equal(plain.providerIdentity.instanceId,selected.instanceId);assert.equal(plain.providerIdentity.accountLabel,'');
  const native={instanceId:selected.instanceId,driver:'codex',displayName:'Work instance',checkedAt:stamp,
    auth:{label:'Native reported account',email:'not copied@example.invalid'},configuration:{secret:'must not leave'}};
  const identity=providerIdentity(native,selected.instanceId);
  assert.deepEqual(Object.keys(identity).sort(),['accountLabel','accountReportedAt','driver','instanceId','label']);
  assert.equal(identity.accountReportedAt,stamp);
  const server=new T3({environmentId:'env-a'},()=>{});t.after(()=>server.close());
  server.current=plain;server.rememberProvider(identity);
  identity.accountLabel='changed caller';assert.equal(server.current.providerIdentity.accountLabel,'Native reported account');
  const projected=modelView(snapshot(),'thread-a',server.providerIdentities);
  projected.providerIdentity.accountLabel='changed projection';
  assert.equal(server.providerIdentities.get(selected.instanceId).accountLabel,'Native reported account');
  const shell={projects:[{id:'project-a',title:'Project'}],threads:[{...snapshot().projection.thread,updatedAt:stamp}]};
  const page=threadList(shell,'',{providerIdentities:server.providerIdentities});
  assert.equal(page.threads[0].providerIdentity.accountLabel,'Native reported account');
  server.disconnected();assert.equal(server.providerIdentities.size,0);
});

test('opt-in opening trace records stage timing and counters without user or credential payloads',async t=>{
  const f=opening(t),trace=[];f.server.trace=entry=>trace.push(entry);
  await f.server.probe();await f.server.watch('thread-a');f.sync();
  const stages=trace.map(e=>e.stage);
  for(const stage of ['descriptor-start','descriptor-ready','ticket-start','ticket-ready','socket-start','socket-ready','bounded-start','bounded-ready','frame-ready','subscription-synchronized'])assert.ok(stages.includes(stage),stage);
  for(const entry of trace)for(const key of Object.keys(entry))assert.ok(['stage','at','generation','epoch','full','rows'].includes(key),key);
  assert.equal(f.server.stats.descriptors,1);assert.equal(f.server.stats.frames,2);
  assert.ok(!JSON.stringify(trace).includes('Synthetic'));assert.ok(!JSON.stringify(trace).includes('fixture'));
});

test('native Working membership separates background waits, failure, plan review and request attention',()=>{
  const base={latestRunId:'run-a',status:'completed',activeRunId:null,pendingRuntimeRequest:null};
  assert.equal(nativeWorking(base),false);
  assert.equal(nativeWorking({...base,status:'running',latestRunId:null,activeProviderThreadId:null}),false);
  assert.equal(nativeWorking({...base,status:'running'}),true);
  assert.equal(nativeWorking({...base,pendingBackgroundTasks:[{kind:'command',awaited:false}]}),false);
  assert.equal(nativeWorking({...base,pendingBackgroundTasks:[{kind:'monitor',awaited:true}]}),true);
  assert.equal(nativeWorking({...base,pendingBackgroundTasks:[{kind:'subagent'}]}),true);
  assert.equal(nativeWorking({...base,status:'failed',pendingBackgroundTasks:[{kind:'subagent'}]}),false);
  assert.equal(nativeWorking({...base,status:'running',pendingRuntimeRequest:{kind:'approval'}}),false);
  assert.equal(nativeWorking({...base,status:'running',pendingRuntimeRequest:{kind:'user_input',blocking:false}}),true);
  assert.equal(nativeWorking({...base,status:'running'},true),false);
  assert.equal(nativeWorking({...base,interactionMode:'plan',hasActionableProposedPlan:true,pendingBackgroundTasks:[{kind:'subagent'}]}),false);
});

test('only live native-managed child approvals change the parent Working membership',()=>{
  const parent={id:'parent',projectId:'project-a',title:'Parent',updatedAt:stamp,status:'running',latestRunId:'run-parent',
    lineage:{relationshipToParent:null,parentThreadId:null,rootThreadId:'parent'}};
  const child={...parent,id:'child',title:'Child',latestRunId:'run-child',creationSource:'orchestrator',
    lineage:{relationshipToParent:'subagent',parentThreadId:'parent',rootThreadId:'parent'},pendingRuntimeRequest:{id:'request',kind:'approval'}};
  function classify(changes={}) {return threadList({projects:[{id:'project-a'}],threads:[parent,{...child,...changes}]},'',{activeOnly:true}).threads.find(t=>t.id==='parent').inbox}
  assert.equal(classify().working,false);assert.equal(classify().pendingChildRequest,true);
  for(const change of [{status:'completed'},{activityRunStatus:null},{creationSource:'provider'},{pendingRuntimeRequest:{id:'request',kind:'user_input'}}]) {
    assert.equal(classify(change).working,true);assert.equal(classify(change).pendingChildRequest,false);
  }
});

test('server search uses one bounded native RPC, exact shell identity and explicit incompleteness',async t=>{
  const server=new T3({environmentId:'env-a'},()=>{});t.after(()=>server.close());server.descriptor=descriptor;
  const calls=[];
  server.http=async path=>{assert.equal(path,'/api/orchestration/shell');return {projects:[{id:'project-a',title:'P'}],threads:[
    {id:'thread-a',title:'No matching title',projectId:'project-a',updatedAt:stamp,status:'completed',modelSelection:selected}]}};
  server.rpc={close(){},call(tag,args){calls.push({tag,args});return {promise:Promise.resolve({matches:[{threadId:'thread-a',projectId:'project-a',source:'assistant',snippet:'needle',messageCreatedAt:null}]})}}};
  server.bounded=()=>{throw new Error('No per-row transcript read during server search')};
  const result=await server.list('needle',{serverSearch:true,limit:50});
  assert.deepEqual(calls,[{tag:'orchestration.searchThreads',args:{query:'needle',limit:50}}]);
  assert.equal(result.threads[0].id,'thread-a');assert.equal(result.searchIncomplete,true);assert.equal(result.serverSearch,true);
  assert.equal(result.threads[0].providerInstanceId,'instance-a');
  server.rpc.call=()=>({promise:Promise.reject(new Fault('unsupported','Native search unavailable'))});
  await assert.rejects(server.list('needle',{serverSearch:true}),{code:'unsupported'});
  assert.equal(server.receipts.size,0);
});
