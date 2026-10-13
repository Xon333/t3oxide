import test from 'node:test';
import assert from 'node:assert/strict';
import { PassThrough, Writable } from 'node:stream';
import { mkdtemp, writeFile, rm, symlink } from 'node:fs/promises';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { run } from '../adapter/main.mjs';
import { LIMIT } from '../adapter/core.mjs';
import { T3 } from '../adapter/t3.mjs';
import { Journal, journalScope } from '../adapter/journal.mjs';

const tick = () => new Promise(resolve => setImmediate(resolve));
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
class Output extends Writable {
  packets = []; held = []; blocked = false;
  constructor() { super({highWaterMark:1}); }
  _write(chunk, _encoding, done) {
    this.packets.push(JSON.parse(chunk.toString()));
    if (this.blocked) this.held.push(done); else done();
  }
  async release() {
    this.blocked = false;
    for (const done of this.held.splice(0)) done();
    await tick();
  }
}
async function session(t, createBackend) {
  const dir = await mkdtemp(join(tmpdir(),'t3oxide-scheduling-'));
  const token = join(dir,'token'), config = join(dir,'host.json');
  await writeFile(token,'fixture-token',{mode:0o600});
  await writeFile(config,JSON.stringify({baseUrl:'http://127.0.0.1:1',environmentId:'env-a',tokenFile:token,allowWrites:true}),{mode:0o600});
  const input = new PassThrough(), output = new Output(), failures = [];
  let emit, closed = 0;
  const handle = await run(['--config',config], {input,output,onFailure:e=>failures.push(e),
    createBackend(c, callback) {
      emit = callback;
      const b = createBackend(c, callback), close = b.close?.bind(b);
      b.close = () => { closed++; close?.(); };
      return b;
    }});
  t.after(async()=>{ handle.close(); input.destroy(); output.destroy(); await rm(dir,{recursive:true,force:true}); });
  const request = (op,id,extra={}) => ({v:1,environmentId:'env-a',op,id,threadId:'thread-a',...extra});
  return { input, output, failures, emit:(...args)=>emit(...args), closed:()=>closed,
    send:(op,id,extra)=>input.write(JSON.stringify(request(op,id,extra))+'\n'),
    batch:entries=>input.write(entries.map(([op,id,extra])=>JSON.stringify(request(op,id,extra))).join('\n')+'\n'),
    reply:id=>output.packets.find(p=>p.id===id) };
}

test('run routes Stop around a stalled read, retaining protocol and operation IDs', async t => {
  const read = deferred(), started = [];
  const h = await session(t,()=>({list:()=>{started.push('read');return read.promise;},
    act:async(op,a)=>{started.push([op,a.id,a.threadId,a.runId]);return {sequence:21};}}));
  h.send('list','read-id'); await tick();
  h.send('send','send-id',{message:'next'});
  h.send('stop','stop-id',{runId:'run-reviewed'}); await tick();
  assert.deepEqual(started,['read',['stop','stop-id','thread-a','run-reviewed']]);
  assert.deepEqual(h.reply('stop-id'),{v:1,environmentId:'env-a',id:'stop-id',result:{sequence:21}});
  assert.equal(h.reply('read-id'),undefined); assert.equal(h.reply('send-id'),undefined);
  read.resolve({threads:[]}); await tick();
  assert.equal(started.at(-1)[0],'send'); assert.ok(h.reply('send-id').result);
});

test('run with durable T3 act dispatches Stop before a stalled send ACK; other mutations stay ordered', async t => {
  const ack = deferred(), calls = [];
  const h = await session(t,(config,emit)=>{
    const b = new T3(config,emit,{journal:new Journal(config.journalDirectory,journalScope(config))});
    b.connect = async()=>{b.descriptor={capabilities:{serverResolvedCommandContext:true}};};
    b.bounded = async()=>({snapshotSequence:10,projection:{thread:{id:'thread-a',projectId:'p',modelSelection:{model:'m'}},
      runs:[{id:'run-a',status:'running'}],visibleTurnItems:[],runtimeRequests:[]}});
    b.rpc = {close(){},call(tag,payload){
      calls.push({tag,payload});
      return {promise:payload.commandId==='send-a'?ack.promise:Promise.resolve(
        tag==='orchestration.launchThread'?{threadId:payload.threadId}:{sequence:22}),cancel(){}};
    }};
    return b;
  });
  h.send('send','send-a',{message:'first'}); await tick();
  h.batch([['send','send-b',{message:'second'}],['launch','launch-c',{message:'third',title:'Task'}],['stop','stop-a',{runId:'run-a'}]]);
  await tick();
  assert.deepEqual(calls.map(c=>c.payload.commandId),['send-a','stop-a']);
  assert.equal(calls[1].payload.type,'run.interrupt'); assert.equal(calls[1].payload.holdQueue,true);
  assert.equal(h.reply('stop-a').result.sequence,22); assert.equal(h.reply('send-a'),undefined);
  ack.resolve({sequence:20}); await tick();
  assert.deepEqual(calls.map(c=>c.payload.commandId),['send-a','stop-a','send-b','launch-c']);
  assert.equal(h.reply('send-a').result.sequence,20);
  assert.equal(h.reply('launch-c').result.threadId,'launch-c');
});

test('admission bounds total work and reserves Stop capacity even when normal queue is full', async t => {
  const read = deferred(), stop = deferred(), calls = [];
  const h = await session(t,()=>({list:()=>read.promise,act:(op,a)=>{
    calls.push(a.id); return a.id==='stop-1'?stop.promise:Promise.resolve({sequence:1});
  }}));
  h.batch(Array.from({length:9},(_,i)=>['list',`list-${i}`]));
  h.batch([['stop','stop-1'],['stop','stop-2'],['stop','stop-3']]); await tick();
  assert.deepEqual(calls,['stop-1']);
  for (const id of ['list-8','stop-3']) {
    assert.equal(h.reply(id).error.code,'backpressure'); assert.equal(h.reply(id).error.uncertain,false);
  }
  assert.equal(h.closed(),0); assert.equal(h.failures.length,0);
  stop.resolve({sequence:1}); await tick();
  assert.deepEqual(calls,['stop-1','stop-2']);
  read.resolve({threads:[]}); await tick();
  assert.equal(h.output.packets.filter(p=>p.id?.startsWith('list-')&&p.result).length,8);
});

test('superseding navigation aborts active work at admission and never starts a queued stale page', async t => {
  let pending, cancellations = 0; const installed = [], starts = [];
  const h = await session(t,()=>({
    cancelNavigation(){cancellations++;pending?.reject(new Error('aborted read'));pending=null;},
    async watch(id){starts.push(id);if(id==='old'){pending=deferred();await pending.promise;}installed.push(id);return {threadId:id};},
    history(){throw new Error('superseded queued history must not execute');}
  }));
  h.send('watch','old-navigation',{threadId:'old'}); await tick();
  h.batch([['history','queued-history',{cursor:'opaque'}],['watch','new-navigation',{threadId:'new'}]]); await tick();
  assert.equal(cancellations,3); assert.deepEqual(starts,['old','new']); assert.deepEqual(installed,['new']);
  for(const id of ['old-navigation','queued-history']) {
    assert.equal(h.reply(id).error.code,'cancelled');assert.equal(h.reply(id).error.uncertain,false);
  }
  assert.equal(h.reply('new-navigation').result.threadId,'new');
});

test('superseded navigation cannot return an empty successful result after late completion', async t => {
  const old = deferred(); let cancelled = 0;
  const h = await session(t,()=>({cancelNavigation(){cancelled++;},watch:id=>id==='old'?old.promise:Promise.resolve({threadId:id})}));
  h.send('watch','old',{threadId:'old'});await tick();
  h.send('watch','new',{threadId:'new'});old.resolve(null);await tick();
  assert.equal(cancelled,2);assert.equal(h.reply('old').error.code,'cancelled');assert.ok(h.reply('new').result);
});

test('Latest cancels an active history read while preserving independent Stop work', async t => {
  let pending; const calls=[];
  const h=await session(t,()=>({
    cancelNavigation(){pending?.reject(new Error('aborted history'));pending=null;},
    history(){pending=deferred();return pending.promise;},
    watch:async id=>({threadId:id}),
    act:async(op,a)=>{calls.push([op,a.id]);return {sequence:31};}
  }));
  h.send('history','history',{cursor:'opaque'});await tick();
  h.batch([['stop','stop',{runId:'reviewed-run'}],['watch','latest']]);await tick();
  assert.equal(h.reply('history').error.code,'cancelled');assert.equal(h.reply('history').error.uncertain,false);
  assert.ok(h.reply('latest').result);assert.equal(h.reply('stop').result.sequence,31);
  assert.deepEqual(calls,[['stop','stop']]);
});

test('invalid navigation identity and rejected admission do not cancel accepted navigation', async t => {
  const read=deferred();let cancelled=0;
  const h=await session(t,()=>({cancelNavigation(){cancelled++;},watch:()=>read.promise}));
  h.send('watch','watch');await tick();
  h.send('watch','wrong-env',{environmentId:'env-b'});
  h.send('history','wrong-version',{v:2,cursor:'opaque'});
  h.batch(Array.from({length:7},(_,i)=>['list',`queued-${i}`]));
  h.send('watch','over-limit');await tick();
  assert.equal(cancelled,1);
  assert.equal(h.reply('wrong-env').error.code,'identity_mismatch');assert.equal(h.reply('wrong-version').error.code,'incompatible');
  assert.equal(h.reply('over-limit').error.code,'backpressure');
  read.resolve({threadId:'thread-a'});await tick();assert.ok(h.reply('watch').result);
});

test('backpressured stdout preserves snapshots, deltas and replies in exact emission order', async t => {
  const h=await session(t,()=>({probe:async()=>({ready:true})}));
  h.output.blocked=true;
  h.emit('view',{threadId:'thread-a',revision:1,items:[]});
  h.emit('delta',{threadId:'thread-a',baseRevision:1,revision:2,upserts:[],removed:[],order:[],metadata:{live:true}});
  h.send('hello','hello');await tick();
  h.emit('view',{threadId:'thread-b',revision:3,items:[]});
  h.emit('delta',{threadId:'thread-b',baseRevision:3,revision:4,upserts:[],removed:[],order:[],metadata:{live:true}});
  assert.equal(h.output.packets.length,1);
  await h.output.release();
  assert.deepEqual(h.output.packets.map(p=>p.event??p.id),['view','delta','hello','view','delta']);
  assert.deepEqual(h.output.packets.filter(p=>p.event).map(p=>p.data.revision),[1,2,3,4]);
  assert.equal(h.failures.length,0);
});

for(const mode of ['frames','bytes']) test(`output ${mode} overflow closes explicitly instead of dropping a delta`,async t=>{
  const h=await session(t,()=>({}));h.output.blocked=true;
  for(let i=0;i<20;i++) h.emit('delta',{revision:i,baseRevision:i-1,
    text:mode==='bytes'?'x'.repeat(LIMIT.frame-200):'small'});
  assert.equal(h.closed(),1);assert.equal(h.failures.length,1);assert.equal(h.failures[0].code,'backpressure');
  assert.equal(h.output.destroyed,true);assert.equal(h.input.destroyed,true);
  assert.equal(h.output.packets.length,1);
});

test('a single oversized output frame closes the pipe without emitting a partial frame',async t=>{
  const h=await session(t,()=>({}));h.emit('view',{text:'x'.repeat(LIMIT.frame)});
  assert.equal(h.failures[0].code,'too_large');assert.equal(h.output.packets.length,0);assert.equal(h.output.destroyed,true);
});

test('input framing accepts split lines and closes on an oversized unterminated request',async t=>{
  const h=await session(t,()=>({probe:async()=>({ready:true})}));
  h.input.write('{"v":1,"id":"partial",');h.input.write('"op":"hello"}\n');await tick();
  assert.ok(h.reply('partial').result);
  h.input.write('x'.repeat(LIMIT.input+1));assert.equal(h.closed(),1);assert.equal(h.failures[0].code,'too_large');
});

test('installed current symlink invokes the adapter entrypoint',async()=>{
  const dir=await mkdtemp(join(tmpdir(),'t3oxide-entry-'));
  try {
    await symlink(fileURLToPath(new URL('../adapter/',import.meta.url)),join(dir,'current'));
    const child=spawnSync(process.execPath,[join(dir,'current/main.mjs'),'--config',join(dir,'missing.json')],{encoding:'utf8',timeout:5000});
    assert.equal(child.status,1,child.error?.message);
    assert.match(child.stderr,/t3oxide: Could not initialize host configuration/);
  } finally {await rm(dir,{recursive:true,force:true});}
});
