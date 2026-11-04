import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {Fault,hash,LIMIT} from '../adapter/core.mjs';
import {T3} from '../adapter/t3.mjs';
import {Journal} from '../adapter/journal.mjs';
import {projectPage,resolveDefaults,launchProjectCommand,nativeRevision,inboxMetadata,threadActionCommand,reviewResult,projectFileDefaults,meaningfulActivity} from '../adapter/native.mjs';
import {threadList} from '../adapter/navigation.mjs';

const NAVIGATION_PIN_TAG='t3oxide:navigation-pin';
const stamp='2026-10-29T10:00:00.000Z';
const project={id:'project-a',title:'Example',workspaceRoot:'/fixture/project',defaultModelSelection:null,defaultThreadEnvMode:null};
const selected={instanceId:'native-account',model:'native-model',options:[{id:'reasoningEffort',value:'medium'}]};
function configuration(){return {settings:{defaultModelSelection:selected,defaultRuntimeMode:'approval-required',defaultThreadEnvMode:'local',projectSettingsFolded:true,projectSettingsOverrides:{}},providers:[{instanceId:'native-account',enabled:true,installed:true,status:'ready',models:[{slug:'native-model',isDefault:true,capabilities:{optionDescriptors:[{id:'reasoningEffort',type:'select',options:[{id:'medium',label:'Medium'},{id:'high',label:'High'}]}]}}]}]};}
function shell(){return {projects:[structuredClone(project)],threads:[]};}
function thread(){return {id:'thread-a',projectId:'project-a',title:'Task',updatedAt:stamp,status:'completed',activeRunId:null,latestRunId:'run-a',worktreePath:'/fixture/tree',branch:'task-a',settledOverride:null,pendingRuntimeRequest:null,modelSelection:selected,runtimeMode:'approval-required',interactionMode:'default'};}
function projection(t=thread()){return {projection:{thread:t,visibleTurnItems:[],runtimeRequests:[],runs:[{id:'run-a',status:'completed'}]}};}
const descriptor={environmentId:'env-a',serverVersion:'0.1.8-nightly.20261104.4179',orchestrationProtocolVersion:2,capabilities:{serverResolvedCommandContext:true}};
function backend({journal,existing=thread()}={}){
  const state={shell:shell(),config:configuration(),calls:[],projects:0,launches:0,projection:projection(existing)};
  if(existing)state.shell.threads.push(existing);
  const server=new T3({environmentId:'env-a',allowWrites:true,token:'fixture'},()=>{}, {journal});
  server.descriptor=descriptor;
  server.http=async path=>{if(path==='/api/orchestration/shell'){state.projects++;return structuredClone(state.shell)};throw new Fault('unavailable','not found')};
  server.bounded=async id=>{if(state.projection?.projection.thread?.id!==id)throw new Fault('unavailable','not found');return structuredClone(state.projection)};
  server.rpc={close(){},call(tag,payload){state.calls.push({tag,payload:structuredClone(payload)});return {promise:(async()=>{
    if(tag==='server.getConfig')return structuredClone(state.config);
    if(tag==='projects.readFile')return {contents:state.projectFile??'{}',truncated:false};
    if(tag==='vcs.listRefs')return {isRepo:true,refs:[{name:'trunk',isDefault:true,current:true}]};
    if(tag==='review.getDiffPreview')return {cwd:payload.cwd,generatedAt:stamp,sources:[{kind:'working-tree',title:'Changes',diff:'diff --git a/a.go b/a.go\n+change',diffHash:'actual-diff-hash',truncated:false,files:[{path:'a.go',additions:1,deletions:0}]}]};
    if(tag==='orchestration.launchThread'){
      state.launches++;
      const t={...thread(),id:payload.threadId,projectId:payload.projectId,title:payload.title,modelSelection:payload.modelSelection,runtimeMode:payload.runtimeMode,interactionMode:payload.interactionMode};
      t.worktreePath=payload.workspaceStrategy.type==='root'?null:'/fixture/worktrees/'+payload.threadId;
      t.branch='native-branch';
      state.projection=projection(t);state.projection.projection.runs=payload.initialMessage?[{id:'native-run',status:'running',userMessageId:payload.initialMessage.messageId}]:[];
      state.shell.threads.push(t);
      if(state.loseLaunch)throw new Fault('timeout','lost launch response',true);
      return {threadId:payload.threadId};
    }
    if(tag==='orchestration.dispatchCommand'){
      const t=state.shell.threads.find(t=>t.id===payload.threadId);
      if(payload.type==='thread.settle'){t.settledOverride='settled';t.pinnedAt=null;t.pinOrderKey=null;}
      if(payload.type==='thread.unsettle')t.settledOverride='active';
      if(payload.type==='thread.snooze')t.snoozedUntil=payload.snoozedUntil;
      if(payload.type==='thread.tag.set'){
        const tags=new Set(t.tags??[]);if(payload.tagged)tags.add(payload.tag);else tags.delete(payload.tag);t.tags=[...tags];
      }else t.updatedAt='2026-10-29T10:01:00.000Z';
      if(state.loseAction)throw new Fault('timeout','lost action response',true);
      return {sequence:42};
    }
    throw new Fault('unsupported','No native fixture handler for '+tag);
  })()}}};
  return {server,state};
}

test('native projects enumerate the first-thread project with bounded paging and exact roots',()=>{
  const s=shell();s.projects.push({...project,id:'project-b'});
  const page=projectPage(s,{limit:1});assert.equal(page.projects[0].id,'project-a');assert.equal(page.nextCursor,1);assert.equal(page.more,true);
  assert.equal(projectPage(s,{cursor:1}).projects[0].id,'project-b');
  assert.throws(()=>projectPage({...s,projects:[project,project]}),{code:'incompatible'});
  assert.throws(()=>projectPage({...s,projects:[{...project,workspaceRoot:'relative'}]}),{code:'incompatible'});
});
test('new native defaults use project/environment settings and never hostile source-thread fields',()=>{
  const s=shell();s.threads=[{...thread(),modelSelection:{instanceId:'foreign',model:'wrong'},runtimeMode:'full-access',worktreePath:'/hostile/source'}];
  const c=configuration();c.settings.projectSettingsOverrides['project-a']={defaultModelSelection:{...selected,options:[{id:'reasoningEffort',value:'high'}]},defaultRuntimeMode:'auto-accept-edits',defaultThreadEnvMode:'worktree'};
  const d=resolveDefaults(s,c,'project-a');assert.equal(d.modelSelection.options[0].value,'high');assert.equal(d.runtimeMode,'auto-accept-edits');assert.equal(d.interactionMode,'default');assert.equal(d.workspaceMode,'worktree');assert.equal(d.workspaceRoot,project.workspaceRoot);
  c.providers[0].enabled=false;assert.throws(()=>resolveDefaults(s,c,'project-a'),{code:'unavailable'});
});
test('native workspace tiers honor t3.json and first-thread scaffold exception',()=>{
  const s=shell(),c=configuration();c.settings.defaultThreadEnvMode=null;
  assert.equal(resolveDefaults(s,c,'project-a',{defaultThreadEnvMode:'worktree'}).workspaceMode,'worktree');
  c.newProjectsRoot='/fixture';c.settings.defaultThreadEnvMode='worktree';
  assert.equal(resolveDefaults(s,c,'project-a').workspaceMode,'local');
  c.settings.projectSettingsOverrides['project-a']={defaultThreadEnvMode:'worktree'};
  assert.equal(resolveDefaults(s,c,'project-a').workspaceMode,'worktree');
});
test('native t3.json JSONC parsing validates known schema without interpreting script text',()=>{
  const file='// repository default\n{ "defaultThreadEnvMode":"worktree", /* shared */ "scripts":[{"name":" Task ","command":"echo https://example.test/a,]/*literal*/",}],}';
  assert.deepEqual(projectFileDefaults(file),{defaultThreadEnvMode:'worktree'});
  for(const invalid of ['{"defaultThreadEnvMode":"guessed"}','{"scripts":[{"name":"x","command":"run","async":"yes"}],"defaultThreadEnvMode":"worktree"}','{"iconPath":" ","defaultThreadEnvMode":"worktree"}','[]','{"defaultThreadEnvMode":"worktree",/*never closed'])assert.equal(projectFileDefaults(invalid),null);
});
test('actual defaults read JSONC when a null project override defers environment mode; configured disabled account never falls back',async()=>{
  const {server,state}=backend();
  try{
    state.config.settings.defaultThreadEnvMode='local';state.config.settings.projectSettingsOverrides['project-a']={defaultThreadEnvMode:null};state.projectFile='{ // inherited repository setting\n "defaultThreadEnvMode":"worktree", }';
    assert.equal((await server.launchDefaults({projectId:'project-a'})).workspaceMode,'worktree');
    assert.equal(state.calls.filter(c=>c.tag==='projects.readFile').length,1);
    state.config.settings.providerInstances={'native-account':{enabled:false}};
    await assert.rejects(server.launchDefaults({projectId:'project-a'}),{code:'unavailable'});
  }finally{server.close()}
});
test('native optional project-file read permits absence without hiding permission or transport failures',async()=>{
  const {server,state}=backend();
  state.config.settings.defaultThreadEnvMode=null;
  const call=server.rpc.call.bind(server.rpc);
  let failure;
  server.rpc.call=(tag,payload)=>{
    if(tag!=='projects.readFile')return call(tag,payload);
    assert.deepEqual(payload,{cwd:project.workspaceRoot,relativePath:'t3.json',allowMissing:true});
    return {promise:failure?Promise.reject(failure):Promise.resolve({exists:false,contents:'',byteLength:0,truncated:false})};
  };
  try{
    assert.equal((await server.launchDefaults({projectId:project.id})).workspaceMode,'local');
    assert.equal((await server.launchDefaults({projectId:project.id,isolated:true})).workspaceStrategy.type,'worktree');
    for(const code of ['authorization','upstream_rejected','timeout','unsupported']){
      failure=new Fault(code,'Project-file read refused');
      await assert.rejects(server.launchDefaults({projectId:project.id}),{code});
    }
  }finally{server.close()}
});
test('untouched native option defaults retained; edited invalid options and stale defaults rejected',()=>{
  const d=resolveDefaults(shell(),configuration(),'project-a');d.workspaceStrategy={type:'root'};d.revision=hash(d);
  const a={id:'op',threadId:'new',newThreadId:'new',projectId:'project-a',title:'  Exact title  ',message:'\nexact\n',defaultsRevision:d.revision,workspaceStrategy:d.workspaceStrategy,workspaceRoot:d.workspaceRoot};
  const command=launchProjectCommand(a,d);assert.equal(command.initialMessage.text,a.message);assert.equal(command.title,a.title);assert.equal(command.threadId,'new');assert.equal(command.projectId,'project-a');assert.equal(command.runtimeMode,'approval-required');
  assert.equal(launchProjectCommand({...a,message:''},d).initialMessage,undefined);
  assert.equal(launchProjectCommand({...a,message:' \n '},d).initialMessage.text,' \n ');
  assert.throws(()=>launchProjectCommand({...a,defaultsRevision:'old'},d),{code:'stale'});
  assert.throws(()=>launchProjectCommand({...a,modelSelection:{...selected,instanceId:'other'}},d),{code:'unsupported'});
  const native={...d,modelSelection:{...d.modelSelection,options:[{id:'nativeOpaque',value:'keep'},{id:'reasoningEffort',value:'medium'}]}};
  native.catalog={...d.catalog,current:native.modelSelection};native.revision=hash(native);
  const edited={...native.modelSelection,options:[{id:'nativeOpaque',value:'keep'},{id:'reasoningEffort',value:'high'}]};
  assert.deepEqual(launchProjectCommand({...a,defaultsRevision:native.revision,modelSelection:edited},native).modelSelection,edited);
  assert.throws(()=>launchProjectCommand({...a,defaultsRevision:native.revision,modelSelection:{...edited,options:[{id:'nativeOpaque',value:'changed'}]}},native),{code:'stale'});
});
test('native metadata preserves exact thread/project/run and native pending request summary',()=>{
  const t=thread();t.pendingRuntimeRequest={id:'request-a',kind:'approval'};t.latestVisibleMessage={role:'assistant',text:'x'.repeat(3000),updatedAt:stamp};
  const meta=inboxMetadata(t);assert.equal(meta.pendingRequestId,'request-a');assert.equal(meta.pendingRequestCount,1);assert.equal(meta.latestMessage.truncated,true);assert.equal(meta.queuedCount,-1);
  const list=threadList({projects:[project],threads:[t]},'',{lifecycle:'all'});assert.equal(list.threads[0].inbox.nativeRevision,meta.nativeRevision);assert.equal(list.threads[0].updatedAt,stamp);
  assert.notEqual(nativeRevision({...t,activeRunId:'new-native-run'}),nativeRevision(t));
});

test('inbox distinguishes an active native run and a completed proposed-plan review',()=>{
  const base={...thread(),latestRunId:'finished-run',activeRunId:null,status:'completed',interactionMode:'plan',hasActionableProposedPlan:true};
  assert.equal(inboxMetadata(base).pendingReview,true);
  assert.equal(inboxMetadata(base).activeRunId,'');
  const running={...base,status:'running',activeRunId:'active-run'};
  assert.equal(inboxMetadata(running).pendingReview,false);
  assert.equal(inboxMetadata(running).activeRunId,'active-run');
  assert.equal(inboxMetadata({...base,interactionMode:'default'}).pendingReview,false);
  assert.equal(inboxMetadata({...base,latestRunCompletedAt:stamp}).latestRunCompletedAt,stamp);
});

test('meaningful activity ignores shell token/tool/presentation churn and qualifies native attention events',()=>{
  const base={...thread(),createdAt:'2026-10-28T10:00:00.000Z',latestRunRequestedAt:'2026-10-29T09:00:00.000Z',latestRunCompletedAt:stamp};
  const completion=meaningfulActivity(base);
  assert.equal(completion.at,stamp);assert.equal(completion.kind,'completion');assert.match(completion.generation,/^[a-f0-9]{64}$/u);
  const churn={...base,updatedAt:'2026-10-29T14:00:00.000Z',title:'A manual name',pinnedAt:'2026-10-29T14:00:00.000Z',itemCount:120};
  assert.deepEqual(meaningfulActivity(churn),completion);
  const running={...churn,status:'running',activeRunId:'new-run',latestRunId:'new-run',latestRunCompletedAt:null,
    latestRunRequestedAt:'2026-10-29T11:00:00.000Z',latestVisibleMessage:{id:'stream',role:'assistant',text:'token',updatedAt:'2026-10-29T14:00:00.000Z'}};
  const before=meaningfulActivity(running);running.latestVisibleMessage.text+=' token';running.updatedAt='2026-10-29T15:00:00.000Z';
  assert.deepEqual(meaningfulActivity(running),before);assert.equal(before.generation,null);assert.equal(before.at,running.latestRunRequestedAt);
  running.pendingRuntimeRequest={id:'request-a',kind:'user_input',createdAt:'2026-10-29T12:00:00.000Z'};
  const need=meaningfulActivity(running);assert.equal(need.kind,'request');assert.equal(need.at,running.pendingRuntimeRequest.createdAt);
  assert.notEqual(meaningfulActivity({...running,pendingRuntimeRequest:{...running.pendingRuntimeRequest,id:'request-b'}}).generation,need.generation);
  const failure=meaningfulActivity({...base,status:'failed',lastError:'provider failed'});
  assert.equal(failure.kind,'failure');assert.notEqual(failure.generation,completion.generation);
  assert.equal(meaningfulActivity({...base,latestRunSilent:true}).generation,null);
});

test('review bounds escaped diff and long file metadata in the actual encoded reply budget',()=>{
  const t=thread();
  const result=reviewResult(t,project,{cwd:t.worktreePath,generatedAt:stamp,sources:[{kind:'working-tree',title:'Working changes',diff:'\u0000'.repeat(128*1024),files:Array.from({length:200},(_,i)=>({path:'dir/'+i+'x'.repeat(4000),additions:1,deletions:0}))}]});
  assert.equal(result.diffTruncated,true);assert.ok(result.files.length>0 && result.files.length<200);
  assert.ok(Buffer.byteLength(JSON.stringify(result))<LIMIT.frame-8192);assert.equal(result.threadId,t.id);assert.equal(result.revision,nativeRevision(t));
});
test('native review/visibility never answers requests or hides active work, and rejects stale revision',()=>{
  const t=thread(),p=projection(t).projection,a={id:'op',threadId:t.id,action:'settle',expectedRevision:nativeRevision(t)};
  assert.equal(threadActionCommand(a,p,t).type,'thread.settle');
  p.runtimeRequests=[{id:'request',status:'pending'}];assert.throws(()=>threadActionCommand(a,p,t),{code:'blocked'});
  p.runtimeRequests=[];p.runs[0].status='running';assert.throws(()=>threadActionCommand(a,p,t),{code:'blocked'});
  p.runs[0].status='completed';assert.throws(()=>threadActionCommand({...a,expectedRevision:'old'},p,t),{code:'stale'});
  assert.throws(()=>threadActionCommand({...a,action:'accept'},p,t));
});
test('actual T3 adapter reads default project and launches isolated native worktree once without source attachment',async()=>{
  const {server,state}=backend({existing:null});
  try{
    const d=await server.launchDefaults({projectId:'project-a',isolated:true});assert.deepEqual(d.workspaceStrategy,{type:'worktree',baseRef:'trunk'});
    const a={id:'launch-one',environmentId:'env-a',threadId:'new-thread',newThreadId:'new-thread',projectId:'project-a',title:'Task',message:'Exact task',defaultsRevision:d.revision,workspaceStrategy:d.workspaceStrategy,workspaceRoot:d.workspaceRoot,isolated:true};
    const result=await server.act('launch-project',a);assert.deepEqual(result,{threadId:'new-thread'});assert.deepEqual(await server.act('launch-project',a),result);assert.equal(state.launches,1);
    const launch=state.calls.find(c=>c.tag==='orchestration.launchThread');assert.equal(launch.payload.initialMessage.messageId,'launch-one:message');assert.equal(server.watchId,null);assert.equal(state.calls.some(c=>c.tag.includes('subscribe')),false);
    await assert.rejects(server.act('launch-project',{...a,id:'other',environmentId:'foreign'}),{code:'identity_mismatch'});
  }finally{server.close()}
});
test('lost launch response reconciles same native thread/message after helper restart without redispatch',async()=>{
  const dir=await mkdtemp(join(tmpdir(),'t3oxide-native-'));
  const journal=new Journal(join(dir,'operations'),hash('fixture-scope'));
  const first=backend({journal,existing:null});
  try{
    const d=await first.server.launchDefaults({projectId:'project-a'});
    const a={id:'saved-launch',environmentId:'env-a',threadId:'new-thread',newThreadId:'new-thread',projectId:'project-a',title:'Task',message:'Exact task',defaultsRevision:d.revision,workspaceStrategy:d.workspaceStrategy,workspaceRoot:d.workspaceRoot};
    first.state.loseLaunch=true;await assert.rejects(first.server.act('launch-project',a),{uncertain:true});assert.equal(first.state.launches,1);
    const second=backend({journal:new Journal(join(dir,'operations'),hash('fixture-scope'))});second.state.projection=structuredClone(first.state.projection);
    try{assert.deepEqual(await second.server.act('launch-project',a),{threadId:'new-thread',reconciled:true});assert.equal(second.state.launches,0)}finally{second.server.close()}
  }finally{first.server.close();await rm(dir,{recursive:true,force:true})}
});
test('lost launch with missing authoritative thread stays uncertain; no retry or new defaults',async()=>{
  const {server,state}=backend({existing:null});
  try{
    const d=await server.launchDefaults({projectId:'project-a'}),a={id:'lost',environmentId:'env-a',threadId:'new',newThreadId:'new',projectId:'project-a',title:'Task',message:'x',defaultsRevision:d.revision,workspaceStrategy:d.workspaceStrategy,workspaceRoot:d.workspaceRoot};
    state.loseLaunch=true;await assert.rejects(server.act('launch-project',a),{uncertain:true});state.projection=null;
    await assert.rejects(server.act('launch-project',a),{uncertain:true});assert.equal(state.launches,1);
  }finally{server.close()}
});
test('actual adapter action journal uses native dispatch and returns post-action revision; review is read-only and bounded',async()=>{
  const {server,state}=backend();
  try{
    const revision=nativeRevision(state.shell.threads[0]);
    const review=await server.reviewThread({threadId:'thread-a',revision});assert.equal(review.files[0].path,'a.go');assert.match(review.diff,/change/);
    const action=await server.act('thread-action',{id:'reviewed',environmentId:'env-a',threadId:'thread-a',action:'settle',expectedRevision:revision});assert.equal(action.sequence,42);assert.equal(action.threadId,'thread-a');assert.equal(action.action,'settle');assert.notEqual(action.nativeRevision,revision);
    await assert.rejects(server.act('thread-action',{id:'stale-undo',environmentId:'env-a',threadId:'thread-a',action:'unsettle',expectedRevision:revision}),{code:'stale'});
    assert.equal(state.calls.filter(c=>c.tag==='orchestration.dispatchCommand').length,1);
    const limited=reviewResult(thread(),project,{cwd:'/fixture/tree',sources:[{kind:'working-tree',title:'Changes',diff:'a'.repeat(200000),diffHash:'x',truncated:false}]});assert.equal(limited.diffTruncated,true);assert.ok(Buffer.byteLength(limited.diff)<=128*1024);
  }finally{server.close()}
});


test('navigation marker changes qualify revision without updatedAt or shelf disposition changes',()=>{
  const t={...thread(),pinnedAt:stamp};
  const tagged={...t,tags:[NAVIGATION_PIN_TAG]};
  assert.notEqual(nativeRevision(t),nativeRevision(tagged));
  assert.equal(inboxMetadata(t).navigationPinned,false);
  assert.equal(inboxMetadata(tagged).navigationPinned,true);
  assert.equal(inboxMetadata(tagged).pinnedAt,stamp);
  const action={id:'original-pin-operation',threadId:t.id,action:'pin',expectedRevision:nativeRevision(t)};
  assert.deepEqual(threadActionCommand(action,projection(t).projection,t),
    {type:'thread.tag.set',commandId:action.id,threadId:t.id,tag:NAVIGATION_PIN_TAG,tagged:true});
  assert.throws(()=>threadActionCommand(action,projection(tagged).projection,tagged),{code:'stale'});
});

test('navigation pin survives reads, run transitions, settle and restore; unpin never changes shelf state',async()=>{
  const {server,state}=backend({existing:{...thread(),pinnedAt:stamp,pinOrderKey:'native-order'}});
  const act=async(action,id)=>server.act('thread-action',{id,environmentId:'env-a',threadId:'thread-a',action,
    expectedRevision:nativeRevision(state.shell.threads[0])});
  try{
    const pinned=await act('pin','pin-intent');
    assert.equal(pinned.metadata.environmentId,'env-a');assert.equal(pinned.metadata.row.inbox.navigationPinned,true);
    assert.equal(pinned.metadata.row.inbox.pinnedAt,stamp);
    await server.bounded('thread-a');
    const t=state.shell.threads[0];t.status='running';t.activeRunId='new-run';
    assert.equal((await server.list('',{lifecycle:'all',includeChildren:true})).threads[0].inbox.navigationPinned,true);
    t.status='completed';t.activeRunId=null;
    const settled=await act('settle','settle-once');
    assert.equal(settled.metadata.row.inbox.settled,true);assert.equal(settled.metadata.row.inbox.pinnedAt,'');
    assert.equal(settled.metadata.row.inbox.navigationPinned,true);
    const restored=await act('unsettle','restore-once');
    assert.equal(restored.metadata.row.inbox.navigationPinned,true);assert.equal(restored.metadata.row.inbox.settled,false);
    t.pinnedAt=stamp;t.pinOrderKey='preserve-shelf';t.settledOverride='settled';
    const unpinned=await act('unpin','remove-intent');
    assert.equal(unpinned.metadata.row.inbox.navigationPinned,false);assert.equal(t.pinnedAt,stamp);
    assert.equal(t.pinOrderKey,'preserve-shelf');assert.equal(t.settledOverride,'settled');
    assert.deepEqual(state.calls.filter(c=>c.tag==='orchestration.dispatchCommand').map(c=>c.payload.type),
      ['thread.tag.set','thread.settle','thread.unsettle','thread.tag.set']);
  }finally{server.close()}
});

test('marker-only external change rejects stale pin preflight, with no inferred tag migration',async()=>{
  const {server,state}=backend({existing:{...thread(),pinnedAt:stamp}});
  try{
    const expectedRevision=nativeRevision(state.shell.threads[0]);
    state.shell.threads[0].tags=[NAVIGATION_PIN_TAG];
    await assert.rejects(server.act('thread-action',{id:'stale-marker',environmentId:'env-a',threadId:'thread-a',action:'pin',expectedRevision}),{code:'stale'});
    assert.equal(state.calls.length,0);
    const list=await server.list('',{lifecycle:'all',includeChildren:true});
    assert.equal(list.threads[0].inbox.navigationPinned,true);
    assert.equal(state.shell.threads[0].updatedAt,stamp);
  }finally{server.close()}
});

test('acknowledged action with failed post-read stays accepted and stale; same-ID lookup reads without redispatch',async()=>{
  const {server,state}=backend();
  const action={id:'post-read-fails',environmentId:'env-a',threadId:'thread-a',action:'pin',expectedRevision:nativeRevision(state.shell.threads[0])};
  const http=server.http;
  server.http=async path=>{if(state.calls.some(c=>c.tag==='orchestration.dispatchCommand'))throw new Fault('transport','post-read failed');return http(path)};
  try{
    for(let attempt=0;attempt<2;attempt++){
      const ack=await server.act('thread-action',action);
      assert.equal(ack.sequence,42);assert.equal(ack.threadId,action.threadId);assert.equal(ack.action,'pin');
      assert.deepEqual(ack.metadata,{environmentId:'env-a',threadId:'thread-a',status:'stale'});
      assert.equal(ack.nativeRevision,undefined);
    }
    assert.equal(state.calls.filter(c=>c.tag==='orchestration.dispatchCommand').length,1);
    server.http=http;
    const fresh=await server.act('thread-action',action);assert.equal(fresh.metadata.status,'fresh');
    state.shell.threads[0].tags=[];
    assert.equal((await server.act('thread-action',action)).metadata.row.inbox.navigationPinned,false);
    assert.equal(state.calls.filter(c=>c.tag==='orchestration.dispatchCommand').length,1);
  }finally{server.close()}
});

test('verified tag support uses release contract, never optional tag presence or shelf pin capability',async()=>{
  for(const version of ['0.1.3','0.1.8-nightly.20261104.4178','unknown','0.1.8-nightly.20261104.4179']){
    const {server,state}=backend();
    server.descriptor={...descriptor,serverVersion:version,capabilities:{serverResolvedCommandContext:true,threadPinning:true}};
    const supported=version==='0.1.8-nightly.20261104.4179';
    try{
      assert.equal((await server.list('',{lifecycle:'all'})).threads[0].inbox.navigationPinned,false);
      const http=server.http;
      server.http=async path=>path==='/.well-known/t3/environment'?server.descriptor:http(path);
      assert.equal((await server.probe()).features.navigationPins,supported);
      const action={id:'explicit-pin',environmentId:'env-a',threadId:'thread-a',action:'pin',expectedRevision:nativeRevision(state.shell.threads[0])};
      if(supported)assert.equal((await server.act('thread-action',action)).metadata.row.inbox.navigationPinned,true);
      else {server.bounded=async()=>{throw new Error('Unsupported Pin must fail before another native read')};await assert.rejects(server.act('thread-action',action),{code:'unsupported'});assert.equal(state.calls.length,0);}
    }finally{server.close()}
  }
});

test('post-action and attached metadata find exact old target beyond pagination and preserve whole source context',async()=>{
  const {server,state}=backend();
  state.shell.threads.unshift(...Array.from({length:250},(_,i)=>({...thread(),id:'recent-'+i,createdAt:'2026-11-04T12:00:00Z'})));
  const t=state.shell.threads.at(-1);t.title='Old target';t.lineage={parentThreadId:'recent-0',rootThreadId:'recent-0',relationshipToParent:'subagent'};
  try{
    const page=await server.list('',{lifecycle:'all',includeChildren:true,limit:200,includeThreadId:t.id});
    assert.equal(page.threads.some(row=>row.id===t.id),false);assert.equal(page.sourceThread.id,t.id);
    const result=await server.act('thread-action',{id:'old-pin',environmentId:'env-a',threadId:t.id,action:'pin',expectedRevision:nativeRevision(t)});
    const fresh=result.metadata.row;
    assert.equal(fresh.id,t.id);assert.equal(fresh.title,'Old target');assert.equal(fresh.projectId,project.id);
    assert.equal(fresh.lineage.parentThreadId,'recent-0');assert.equal(fresh.inbox.navigationPinned,true);
  }finally{server.close()}
});
