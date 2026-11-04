import test from 'node:test';
import assert from 'node:assert/strict';
import { T3 } from '../adapter/t3.mjs';
import { requests } from '../adapter/core.mjs';

function fixture() {
  const projection = {thread:{id:'source-thread'},visibleTurnItems:[],turnItems:[{
    id:'question-item',requestId:'question-request',type:'user_input_request',
    questions:[{id:'release_scope',header:'Release scope',required:true,multiSelect:false,
      options:[{id:'stable',label:'Stable',value:'stable'},{id:'next',label:'Next',value:'next'}]}]
  }],runtimeRequests:[{id:'question-request',status:'pending',responseCapability:{type:'live'}}],runs:[]};
  const calls = [], server = new T3({environmentId:'source-environment',allowWrites:true},()=>{});
  server.connect = async()=>{};
  server.bounded = async()=>({projection:structuredClone(projection)});
  server.rpc = {close(){},call(tag,payload){calls.push(structuredClone(payload));return {promise:Promise.resolve({sequence:23}),cancel(){}};}};
  const action = {id:'original-operation',environmentId:'source-environment',threadId:'source-thread',
    requestId:'question-request',revision:requests(projection)[0].revision,answers:{release_scope:'stable'}};
  return {server,projection,calls,action};
}

test('changed question wording or choice value rejects the original revision without dispatch',async()=>{
  for (const change of [
    p=>p.turnItems[0].questions[0].question='Changed wording',
    p=>p.turnItems[0].questions[0].options[0].value='changed-value',
    p=>p.runtimeRequests[0].status='cancelled'
  ]) {
    const {server,projection,calls,action}=fixture();
    try {change(projection);await assert.rejects(server.act('answer',action),{code:'stale'});assert.equal(calls.length,0);}
    finally {server.close();}
  }
});

test('invalid native answer values and duplicate selections fail before dispatch',async()=>{
  for (const answers of [{release_scope:'not-offered'},{release_scope:[]},{release_scope:''},{unknown:'stable'},{}]) {
    const {server,calls,action}=fixture();
    try {await assert.rejects(server.act('answer',{...action,answers}));assert.equal(calls.length,0);}
    finally {server.close();}
  }
  const {server,projection,calls,action}=fixture();
  projection.turnItems[0].questions[0].multiSelect=true;
  action.revision=requests(projection)[0].revision;
  try {await assert.rejects(server.act('answer',{...action,answers:{release_scope:['stable','stable']}}));assert.equal(calls.length,0);}
  finally {server.close();}
});

test('native answer acceptance identifies the original source, revision and operation',async()=>{
  const {server,calls,action}=fixture();
  try {
    const result=await server.act('answer',action);
    assert.equal(calls.length,1);
    assert.equal(calls[0].commandId,action.id);
    assert.equal(calls[0].requestId,action.requestId);
    assert.equal(result.sequence,23);
    assert.deepEqual(result.acknowledgement,{environmentId:action.environmentId,threadId:action.threadId,
      requestId:action.requestId,revision:action.revision,operationId:action.id});
  } finally {server.close();}
});

test('unsupported native choice types do not fall back to labels or dispatch',async()=>{
  for (const value of [42, null, {label:'unexpected'}]) {
    const {server,projection,calls,action}=fixture();
    projection.turnItems[0].questions[0].options[0].value=value;
    action.revision=requests(projection)[0].revision;
    try {await assert.rejects(server.act('answer',action),{code:'unsupported'});assert.equal(calls.length,0);}
    finally {server.close();}
  }
});
