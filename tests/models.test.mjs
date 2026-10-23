import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { selection, sameSelection, catalog, validateChoice, launchCommand, modelCommand, modelView, runtimeDetails } from '../adapter/models.mjs';
import { Fault, hash } from '../adapter/core.mjs';
import { Journal } from '../adapter/journal.mjs';
import { T3 } from '../adapter/t3.mjs';

const current = {instanceId:'provider-a',model:'model-a',options:[{id:'reasoningEffort',value:'medium'}]};
const desc = {capabilities:{serverResolvedCommandContext:true}};
function projection() { return {thread:{id:'thread-a',title:'Alpha',projectId:'project',modelSelection:structuredClone(current)},
  runs:[{id:'run-a',status:'running',providerThreadId:'pt-a',modelSelection:{...current,model:'run-model'}}],
  providerThreads:[{id:'pt-a',providerSessionId:'ps-a',nativeMetadata:{modelSelection:{...current,model:'reported-model'},updatedAt:'2026-10-22T13:00:00Z'}}],
  providerSessions:[{id:'ps-a',model:'session-model'}],visibleTurnItems:[],turnItems:[],runtimeRequests:[]}; }
function sourceCatalog() {return {secret:'must-not-leave-host',providers:[{instanceId:'provider-a',name:'Provider',status:'ready',auth:{status:'authenticated',token:'secret'},
  models:[
    {slug:'model-a',name:'Model A',isDefault:true,capabilities:{optionDescriptors:[
      {id:'reasoningEffort',label:'Reasoning',type:'select',currentValue:'medium',options:[{id:'medium',label:'Medium',isDefault:true},{id:'high',label:'High'}]},
      {id:'serviceTier',label:'Service Tier',type:'select',options:[{id:'default',label:'Standard'},{id:'fast',label:'Fast'}]},
    ]}},
    {slug:'model-b',name:'Model B',capabilities:{optionDescriptors:[{id:'fastMode',label:'Fast',type:'boolean',currentValue:false}]}},
  ]}]};}
const action = {id:'model-op',environmentId:'env-a',threadId:'thread-a',expectedModelSelection:current,
  modelSelection:{...current,options:[{id:'reasoningEffort',value:'high'}]}};

test('canonical model options accept legacy maps without inventing values', () => {
  assert.deepEqual(selection({...current,options:{serviceTier:'fast',reasoningEffort:'high'}}).options,
    [{id:'reasoningEffort',value:'high'},{id:'serviceTier',value:'fast'}]);
  assert.equal(sameSelection({...current,options:[]},{instanceId:'provider-a',model:'model-a'}),true);
  assert.equal(sameSelection({...current,options:{reasoningEffort:'medium'}},current),true);
  assert.deepEqual(selection({instanceId:'provider-a',model:'model-b'}),{instanceId:'provider-a',model:'model-b'});
});
test('model identities, option types, duplicates, controls and byte budgets fail closed', async t => {
  for (const [name,value] of Object.entries({
    missing:{model:'model-a'},number:{...current,options:[{id:'x',value:3}]},nested:{...current,options:[{id:'x',value:{}}]},
    duplicate:{...current,options:[{id:'x',value:true},{id:'x',value:false}]},control:{...current,model:'bad\u001bname'},
    whitespace:{...current,model:' model-a'},unicodeBytes:{...current,model:'漢'.repeat(86)},
    nullOptions:{...current,options:null},blankOption:{...current,options:[{id:' ',value:true}]},
    spacedOption:{...current,options:[{id:' effort',value:'high'}]},
    spacedValue:{...current,options:[{id:'effort',value:' high'}]},
    spacedProvider:{...current,instanceId:' provider-a'},
    tooMany:{...current,options:Array.from({length:65},(_,i)=>({id:`o${i}`,value:true}))},
    total:{...current,options:Array.from({length:20},(_,i)=>({id:`o${i}`,value:'x'.repeat(1000)}))}
  })){await t.test(name,()=>assert.throws(()=>selection(value)));}
});
test('catalog exposes only current provider descriptors and no config/auth payload', () => {
  const c=catalog(sourceCatalog(),projection());
  assert.equal(c.available,true);assert.equal(c.models.length,2);assert.equal(c.models[0].options[0].currentValue,'medium');
  assert.equal(c.models[1].options[0].currentValue,false);
  assert.doesNotMatch(JSON.stringify(c),/secret|token|must-not/);
  assert.equal(c.current.options[0].value,'medium');
});
test('catalog keeps unsupported future controls hidden and marks malformed models omitted', () => {
  const cfg=sourceCatalog();
  cfg.providers[0].models.push({slug:'bad\u0001model',name:'Bad'});
  cfg.providers[0].models[0].capabilities.optionDescriptors.push({id:'future',type:'range'});
  const c=catalog(cfg,projection());
  assert.equal(c.models.length,2);assert.equal(c.more,true);assert.equal(c.models[0].options.length,2);
});
test('catalog honors provider display identity, disabled and uninstalled states', () => {
  const cfg=sourceCatalog(),provider=cfg.providers[0];
  provider.displayName='Selected account';
  assert.equal(catalog(cfg,projection()).providerLabel,'Selected account');
  for (const field of ['enabled','installed']) {
    provider[field]=false;
    const c=catalog(cfg,projection());
    assert.equal(c.available,false);
    assert.throws(()=>validateChoice(action.modelSelection,current,c),{code:'unavailable'});
    provider[field]=true;
  }
});
test('provider model-change restriction rejects both saved and one-dispatch changes', () => {
  const cfg=sourceCatalog(),p=projection();
  cfg.providers[0].requiresNewThreadForModelChange=true;
  const c=catalog(cfg,p),next={instanceId:'provider-a',model:'model-b'};
  assert.equal(c.requiresNewThreadForModelChange,true);
  assert.throws(()=>modelCommand('select-model',{...action,modelSelection:next},p,desc,c),{code:'unsupported'});
  assert.throws(()=>modelCommand('send',{...action,message:'text',modelSelection:next},p,desc,c),{code:'unsupported'});
  assert.deepEqual(validateChoice(action.modelSelection,current,c),selection(action.modelSelection));
});
test('catalog is bounded in entries and bytes, never truncates a selectable identity', () => {
  const cfg=sourceCatalog();const original=cfg.providers[0].models[0];
  cfg.providers[0].models=Array.from({length:260},(_,i)=>({...original,slug:`model-${i}`}));
  const c=catalog(cfg,projection()); assert.equal(c.models.length,200);assert.equal(c.more,true);
  assert.ok(Buffer.byteLength(JSON.stringify(c))<256*1024);
  assert.equal(c.models.at(-1).slug,'model-199');
});
test('choices are provider-specific and validated against current catalog, not a universal effort list', async t => {
  const c=catalog(sourceCatalog(),projection());
  assert.equal(validateChoice(action.modelSelection,current,c).options[0].value,'high');
  assert.deepEqual(validateChoice({instanceId:'provider-a',model:'model-b',options:[{id:'fastMode',value:false}]},current,c).options,[{id:'fastMode',value:false}]);
  for(const [name,next] of Object.entries({unknownModel:{...current,model:'missing'},otherProvider:{...current,instanceId:'provider-b'},
    unsupportedEffort:{...current,options:[{id:'reasoningEffort',value:'invented'}]},wrongType:{...current,options:[{id:'reasoningEffort',value:true}]},
    wrongModel:{...current,model:'model-b'},booleanString:{instanceId:'provider-a',model:'model-b',options:[{id:'fastMode',value:'true'}]}})) {
    await t.test(name,()=>assert.throws(()=>validateChoice(next,current,c)));
  }
  c.available=false;assert.throws(()=>validateChoice(current,current,c),{code:'unavailable'});
});
test('unchanged provider-specific options survive same-model effort edits',()=>{
  const before={...current,options:[...current.options,{id:'provider-private-option',value:'keep'}]},c=catalog(sourceCatalog(),projection());
  const next={...before,options:[{id:'reasoningEffort',value:'high'},{id:'provider-private-option',value:'keep'}]};
  assert.equal(validateChoice(next,before,c).options.find(o=>o.id==='provider-private-option').value,'keep');
  assert.throws(()=>validateChoice({...next,options:[{id:'provider-private-option',value:'new'}]},before,c));
});
test('model mutation has exact upstream command, stale preflight rejection and original send provenance',()=>{
  const p=projection(),c=catalog(sourceCatalog(),p);
  const cmd=modelCommand('select-model',action,p,desc,c);
  assert.deepEqual(cmd,{type:'thread.model-selection.set',commandId:'model-op',threadId:'thread-a',modelSelection:selection(action.modelSelection)});
  assert.throws(()=>modelCommand('select-model',{...action,expectedModelSelection:{...current,model:'changed'}},p,desc,c),{code:'stale'});
  const send=modelCommand('send',{...action,id:'send-op',message:'human text',mode:'queue'},p,desc,c);
  assert.equal(send.type,'message.dispatch');assert.equal(send.createdBy,'user');assert.equal(send.creationSource,'web');
  assert.equal(send.dispatchMode.type,'queue_after_active');assert.equal(send.deliveryIntent,undefined);
  assert.deepEqual(send.modelSelection,selection(action.modelSelection));
  const automatic=modelCommand('send',{...action,id:'send-auto',message:'human text',mode:'auto'},p,desc,c);
  assert.equal(automatic.deliveryIntent,'auto');
  const ordinary=modelCommand('send',{id:'ordinary',threadId:'thread-a',message:'unchanged'},p,desc,null);
  assert.equal(Object.hasOwn(ordinary,'modelSelection'),false);
});
test('runtime separates saved, run-requested, provider-reported and session-only evidence',()=>{
  const p=projection(),view=modelView({projection:p,snapshotSequence:12},'thread-a');
  assert.equal(view.modelSelection.model,'model-a');assert.equal(view.model,'model-a');
  assert.equal(view.runtimeDetails.scope,'active-run');assert.equal(view.runtimeDetails.runModelSelection.model,'run-model');
  assert.equal(view.runtimeDetails.providerReportedSelection.model,'reported-model');assert.equal(view.runtimeDetails.sessionReportedModel,'session-model');
  assert.equal(view.runtimeDetails.providerReportedAt,'2026-10-22T13:00:00Z');
  p.runs[0].status='completed';assert.equal(runtimeDetails(p).scope,'last-run');
  p.runs.push({id:'new-run',status:'starting',providerThreadId:null});p.thread.activeProviderThreadId='pt-a';
  const next=runtimeDetails(p);assert.equal(next.providerReportedSelection,null);assert.equal(next.runModelSelection,null);
  p.runs=[];assert.equal(runtimeDetails(p).scope,'provider-thread');p.thread.activeProviderThreadId=null;
  assert.equal(runtimeDetails(p).scope,'unreported');
});
test('missing provider telemetry is unreported, not copied from saved settings',()=>{
  const p=projection();delete p.providerThreads[0].nativeMetadata;
  const r=runtimeDetails(p);assert.equal(r.providerReportedSelection,null);assert.equal(r.providerReportedAt,null);
  delete p.runs[0].modelSelection;assert.equal(runtimeDetails(p).runModelSelection,null);
});
test('run message identity comes only from the selected native run, never a queued user row',()=>{
  const p=projection();p.runs[0].userMessageId='old-send:message';
  p.runs.push({id:'queued-run',status:'queued',userMessageId:'new-send:message'});
  p.visibleTurnItems=[{item:{id:'new-row',threadId:'thread-a',type:'user_message',messageId:'new-send:message',text:'Next'}}];
  const active=runtimeDetails(p);
  assert.equal(active.runId,'run-a');assert.equal(active.runMessageId,'old-send:message');assert.equal(active.status,'running');
  p.runs.pop();p.runs[0].status='completed';
  const last=runtimeDetails(p);assert.equal(last.scope,'last-run');assert.equal(last.runMessageId,'old-send:message');
  assert.equal(modelView({projection:p,snapshotSequence:12},'thread-a').runtimeDetails.runMessageId,'old-send:message');
  for(const value of [undefined,null,'','bad\u0000id','x'.repeat(4097),{id:'invented'}]) {
    p.runs[0].userMessageId=value;assert.equal(runtimeDetails(p).runMessageId,null);
  }
  p.runs=[];assert.equal(runtimeDetails(p).runMessageId,null);
});
test('launch validation permits new-thread-only models but rejects stale options even when inherited',()=>{
  const p=projection(),cfg=sourceCatalog();cfg.providers[0].requiresNewThreadForModelChange=true;
  const c=catalog(cfg,p),a={...action,title:'New',message:'Task',modelSelection:{instanceId:'provider-a',model:'model-b',options:[{id:'fastMode',value:true}]}};
  assert.deepEqual(launchCommand(a,p,desc,c).modelSelection,a.modelSelection);
  assert.throws(()=>launchCommand(a,p,{capabilities:{}},c),{code:'incompatible'});
  p.thread.modelSelection.options.push({id:'removed-option',value:'old'});
  assert.throws(()=>launchCommand({...a,modelSelection:p.thread.modelSelection},p,desc,c),{code:'stale'});
  assert.deepEqual(launchCommand({...a,modelSelection:undefined},p,desc,null).modelSelection,p.thread.modelSelection);
});

async function durableFixture(t) {
  const dir=await mkdtemp(join(tmpdir(),'t3oxide-models-'));t.after(()=>rm(dir,{recursive:true,force:true}));
  const state={p:projection(),config:sourceCatalog(),calls:[],tags:[],reads:0,catalogReads:0,fail:false};
  const backend=()=>{
    const journal=new Journal(join(dir,'journal'),hash({environment:'env-a',subject:'fixture-models'}));
    const server=new T3({environmentId:'env-a',allowWrites:true},()=>{},{journal});
    server.descriptor=desc;server.connect=async()=>{};
    server.bounded=async()=>{state.reads++;return {projection:structuredClone(state.p),snapshotSequence:12};};
    server.rpc={close(){},call(tag,payload){
      if(tag==='server.getConfig'){state.catalogReads++;return {promise:Promise.resolve(structuredClone(state.config))};}
      state.calls.push(structuredClone(payload));
      state.tags.push(tag);
      if(payload.type==='thread.model-selection.set')state.p.thread.modelSelection=structuredClone(payload.modelSelection);
      return {promise:state.fail?Promise.reject(new Fault('disconnected','Lost ACK',true)):
        Promise.resolve(tag==='orchestration.launchThread'?{threadId:payload.threadId}:{sequence:13})};
    }};
    t.after(()=>server.close());return server;
  };return {state,backend};
}
test('uncertain model receipt survives helper restart and bypasses stale catalog on explicit retry',async t=>{
  const {state,backend}=await durableFixture(t);let s=backend();state.fail=true;
  await assert.rejects(s.act('select-model',action),{uncertain:true});s.close();
  state.config.providers=[];state.p.thread.modelSelection={instanceId:'different',model:'newer'};
  s=backend();state.fail=false;assert.deepEqual(await s.act('select-model',action),{sequence:13});
  assert.equal(state.reads,1);assert.equal(state.catalogReads,1);assert.deepEqual(state.calls[0],state.calls[1]);
  assert.equal(state.calls[1].commandId,action.id);
  await assert.rejects(s.act('select-model',{...action,modelSelection:current}),{code:'invalid_request'});
});
test('accepted model receipt survives helper restart without redispatch',async t=>{
  const {state,backend}=await durableFixture(t);let s=backend();await s.act('select-model',action);s.close();
  state.config.providers=[];s=backend();assert.deepEqual(await s.act('select-model',action),{sequence:13});
  assert.equal(state.calls.length,1);assert.equal(state.catalogReads,1);
});
test('one-dispatch model override remains exact through uncertain retry',async t=>{
  const {state,backend}=await durableFixture(t);const a={...action,id:'override-send',message:'once',mode:'auto',modelOverrideId:'local-draft-selection'};
  let s=backend();state.fail=true;await assert.rejects(s.act('send',a),{uncertain:true});s.close();
  state.config.providers=[];state.fail=false;s=backend();await s.act('send',a);
  assert.deepEqual(state.calls[0],state.calls[1]);assert.equal(state.calls[1].modelSelection.options[0].value,'high');
  assert.equal(state.calls[1].modelOverrideId,undefined);assert.equal(state.catalogReads,1);
});
test('model acknowledgment storage failure remains uncertain, recoverable and never auto-replayed',async t=>{
  const {state,backend}=await durableFixture(t);let s=backend();s.journal.acknowledge=()=>{throw new Error('disk full');};
  await assert.rejects(s.act('select-model',action),{uncertain:true});s.close();
  s=backend();await new Promise(resolve=>setImmediate(resolve));assert.equal(state.calls.length,1);
  assert.deepEqual(await s.act('select-model',action),{sequence:13});assert.equal(state.calls.length,2);
});
const launchAction={id:'launch-op',environmentId:'env-a',threadId:'thread-a',title:' Human title ',message:'\nExact first message\n',
  modelSelection:{instanceId:'provider-a',model:'model-b',options:[{id:'fastMode',value:true}]}};
test('launch dispatches selected model once with exact source account/worktree/modes and no source configuration',async t=>{
  const {state,backend}=await durableFixture(t);
  Object.assign(state.p.thread,{worktreePath:'/fixture/existing-worktree',branch:'fixture-branch',runtimeMode:'approval-required',interactionMode:'default'});
  state.config.providers[0].requiresNewThreadForModelChange=true;
  const original=structuredClone(state.p),s=backend();
  assert.deepEqual(await s.act('launch',launchAction),{threadId:launchAction.id});
  assert.deepEqual(state.tags,['orchestration.launchThread']);assert.equal(state.reads,1);assert.equal(state.catalogReads,1);
  assert.deepEqual(state.calls,[{commandId:launchAction.id,creationSource:'web',threadId:launchAction.id,
    projectId:'project',title:launchAction.title,modelSelection:launchAction.modelSelection,
    runtimeMode:'approval-required',interactionMode:'default',
    workspaceStrategy:{type:'existing_worktree',worktreePath:'/fixture/existing-worktree',branch:'fixture-branch'},
    initialMessage:{messageId:`${launchAction.id}:message`,text:launchAction.message,attachments:[]}}]);
  assert.deepEqual(state.p,original);
});
test('inherited launch stays compatible without catalog reads and preserves root binding',async t=>{
  const {state,backend}=await durableFixture(t),s=backend();state.config.providers=[];
  const a={...launchAction,modelSelection:undefined};
  await s.act('launch',a);
  assert.equal(state.catalogReads,0);assert.deepEqual(state.calls[0].workspaceStrategy,{type:'root'});
  assert.deepEqual(state.calls[0].modelSelection,current);assert.deepEqual(state.tags,['orchestration.launchThread']);
});
test('fresh launch rejects missing model, stale options, changed account, and unavailable provider without a mutation',async t=>{
  for(const [name,change] of Object.entries({
    model:state=>{state.config.providers[0].models.pop();},
    option:state=>{state.config.providers[0].models[1].capabilities.optionDescriptors=[];},
    value:state=>{state.config.providers[0].models[1].capabilities.optionDescriptors=[{id:'fastMode',type:'select',options:[{id:'default'}]}];},
    account:state=>{state.p.thread.modelSelection.instanceId='another-account';state.config.providers[0].instanceId='another-account';},
    unavailable:state=>{state.config.providers[0].enabled=false;},
    archived:state=>{state.p.thread.archivedAt='now';},
  })) await t.test(name,async t=>{
    const {state,backend}=await durableFixture(t);change(state);
    await assert.rejects(backend().act('launch',launchAction));assert.deepEqual(state.calls,[]);
  });
});
test('uncertain launch restart retries the exact journalled payload without reading changed context or catalog',async t=>{
  const {state,backend}=await durableFixture(t);let s=backend();state.fail=true;
  await assert.rejects(s.act('launch',launchAction),{uncertain:true});
  const prepared=s.journal.access(launchAction.id,hash({op:'launch',...launchAction}));
  assert.equal(prepared.result,undefined);assert.equal(prepared.tag,'orchestration.launchThread');
  assert.deepEqual(prepared.payload,state.calls[0]);s.close();
  state.config.providers=[];state.p.thread={id:'thread-a',projectId:'different-project',worktreePath:'/fixture/changed',modelSelection:{instanceId:'other-account',model:'changed'}};
  state.fail=false;s=backend();await new Promise(resolve=>setImmediate(resolve));assert.equal(state.calls.length,1);
  assert.deepEqual(await s.act('launch',launchAction),{threadId:launchAction.id});
  assert.equal(state.reads,1);assert.equal(state.catalogReads,1);assert.deepEqual(state.calls[1],prepared.payload);
  assert.deepEqual(s.journal.access(launchAction.id,hash({op:'launch',...launchAction})).result,{threadId:launchAction.id});
  await assert.rejects(s.act('launch',{...launchAction,modelSelection:current}),{code:'invalid_request'});
  s.close();s=backend();assert.deepEqual(await s.act('launch',launchAction),{threadId:launchAction.id});
  assert.equal(state.calls.length,2);assert.equal(state.reads,1);assert.equal(state.catalogReads,1);
});
test('read-only launch selection refuses all reads and writes',async t=>{
  const {state,backend}=await durableFixture(t),s=backend();s.config.allowWrites=false;
  await assert.rejects(s.act('launch',launchAction),{code:'read_only'});
  assert.equal(state.reads,0);assert.equal(state.catalogReads,0);assert.deepEqual(state.calls,[]);
});
