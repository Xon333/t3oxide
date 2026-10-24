import test from 'node:test';
import assert from 'node:assert/strict';
import {activity,view,command} from '../adapter/core.mjs';
import {modelView} from '../adapter/models.mjs';
import {threadList} from '../adapter/navigation.mjs';
const root=i=>({id:`root-${i}`,title:`Match ${i}`,projectId:'p',updatedAt:'2026-10-24',status:'idle',lineage:{relationshipToParent:null,parentThreadId:null}});
const child=(id,status)=>({...root(id),id,status,lineage:{relationshipToParent:'subagent',parentThreadId:'root-0'}});
const shell=threads=>({projects:[{id:'p',title:'Project'}],threads});
const opts={activeOnly:true,grouped:true};
test('quiet default excludes settled/finished children but explicit lifecycle discovers them',()=>{
 const data=shell([root(0),{...root('settled'),settledOverride:'settled'},child('working','running'),child('done','completed'),child('failed','failed')]);
 assert.deepEqual(threadList(data,'',opts).threads.map(t=>t.id),['root-0']);
 assert.deepEqual(threadList(data,'',{...opts,expanded:['root-0']}).threads.map(t=>t.id),['root-0','working']);
 assert.deepEqual(threadList(data,'',{...opts,lifecycle:'completed',typeFilter:'subagents'}).threads.map(t=>t.id),['done']);
 assert.equal(threadList(data,'',{...opts,lifecycle:'all',typeFilter:'subagents'}).threads.length,3);
});
test('search counts include displayed finished children and paging cannot crowd them out',()=>{
 const data=shell([...Array.from({length:200},(_,i)=>root(i)),child('matching-child','completed')]);
 const first=threadList(data,'Match',{...opts,limit:24});
 assert(first.threads.some(t=>t.id==='matching-child'));assert.equal(first.threads[0].childCount,1);assert.equal(first.threads[0].workingChildCount,0);assert.equal(first.threads[0].loadedChildCount,1);
 const seen=new Set(first.threads.map(t=>t.id));let cursor=first.nextCursor;
 while(cursor!==null){const next=threadList(data,'Match',{...opts,limit:24,cursor});for(const t of next.threads){assert(!seen.has(t.id));seen.add(t.id);}cursor=next.nextCursor;}
 assert.equal(seen.size,201);
});
test('native child status correlates provider turns without granting app-run Stop',()=>{
 const snapshot={snapshotSequence:1,projection:{thread:{id:'child',title:'Child',projectId:'p',modelSelection:{model:'m'},lineage:{relationshipToParent:'subagent'},activeProviderThreadId:'pt'},runs:[],runtimeRequests:[],turnItems:[],visibleTurnItems:[],providerThreads:[{id:'pt',appThreadId:'child',status:'active'}],providerTurns:[{id:'turn',providerThreadId:'pt',ordinal:1,status:'running'}]}};
 for(const status of ['running','completed','failed','interrupted']){snapshot.projection.providerTurns[0].status=status;const v=modelView(snapshot,'child');assert.equal(v.status,status);assert.equal(v.runtimeDetails.status,status);assert.equal(v.activeRunId,null);}
 assert.throws(()=>command('stop',{id:'op',threadId:'child',runId:'invented'},snapshot.projection,{}));
 snapshot.projection.providerTurns[0].status='running';snapshot.projection.runtimeRequests=[{status:'pending'}];assert.equal(activity(snapshot.projection).status,'waiting');
 snapshot.projection.thread.activeProviderThreadId='unrelated';assert.equal(activity(snapshot.projection).status,'unreported');
});
test('list scope and cursors fail closed on malformed values',()=>{
 for(const options of [{lifecycle:'guess'},{cursor:-1},{cursor:1.5},{limit:201}])assert.throws(()=>threadList(shell([root(0)]),'',{...opts,...options}));
});
