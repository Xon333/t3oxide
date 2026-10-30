// Controlled slow-Writable NDJSON boundary, identical for baseline/result.
// This is the real adapter run/output scheduler with a synthetic backend; no
// HTTP, terminal renderer, kernel write completion or physical-input claim.
import {PassThrough, Writable} from 'node:stream';
import {mkdtemp,writeFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
import {performance} from 'node:perf_hooks';
import assert from 'node:assert/strict';
const source=resolve(process.argv[2] ?? new URL('..',import.meta.url).pathname);
const {run}=await import(pathToFileURL(resolve(source,'adapter/main.mjs')).href);
const directory=await mkdtemp(join(tmpdir(),'t3oxide-output-performance-'));
const config=join(directory,'host.json'),token=join(directory,'token');
await writeFile(token,'synthetic-test-token',{mode:0o600});
await writeFile(config,JSON.stringify({baseUrl:'http://127.0.0.1:1',environmentId:'synthetic-environment',tokenFile:token,allowWrites:false}),{mode:0o600});
const samples=2000,warmup=100,emittedAt=new Map(),times=[],sequence=[],trace=[];
let completed=0,bytes=0,finish,emit,failures=[];
const input=new PassThrough();
const output=new Writable({highWaterMark:1,write(chunk,_encoding,done){
  const packet=JSON.parse(chunk.toString()),revision=packet.data.revision;sequence.push(revision);bytes+=chunk.length;
  setTimeout(()=>{const elapsed=performance.now()-emittedAt.get(revision);if(revision>warmup)times.push(elapsed);completed++;done();finish?.();},1);
}});
const handle=await run(['--config',config],{input,output,trace:entry=>trace.push(entry),onFailure:e=>failures.push(e.code),createBackend:(_config,send)=>{emit=send;return {close(){}}}});
let emitted=0,maxOutstanding=0;
try {
  for(let batch=0;batch<(samples+warmup)/4;batch++) {
    const target=emitted+4;
    for(let i=0;i<4;i++) {
      const revision=++emitted;emittedAt.set(revision,performance.now());maxOutstanding=Math.max(maxOutstanding,emitted-completed);
      emit(revision===1?'view':'delta',revision===1?{threadId:'synthetic-thread',revision,items:[],requests:[]}:
        {threadId:'synthetic-thread',baseRevision:revision-1,revision,upserts:[],removed:[],order:[],metadata:{sequence:revision}});
    }
    if(completed<target)await new Promise(resolve=>{finish=()=>{if(completed>=target){finish=null;resolve()}}});
  }
  assert.equal(times.length,samples);assert.deepEqual(sequence,Array.from({length:samples+warmup},(_,i)=>i+1));assert.deepEqual(failures,[]);
  const sorted=times.slice().sort((a,b)=>a-b),q=p=>sorted[Math.ceil(samples*p)-1];
  console.log(JSON.stringify({schema:1,workload:'slow-writable-ndjson-v1',node:process.version,samples,warmup,
    boundary:'event emit to controlled Writable callback; four-frame batches; each callback deliberately delayed by timer(1ms); not terminal output/pixels',
    median:q(.5),p95:q(.95),p99:q(.99),worst:sorted.at(-1),mean:times.reduce((a,b)=>a+b,0)/samples,
    exactOrderedFrames:sequence.length,bytes,maxOutstanding,adapterQueueHighWater:trace.length?Math.max(...trace.map(e=>e.queuedFrames)):null,
    adapterBytesHighWater:trace.length?Math.max(...trace.map(e=>e.queuedBytes)):null,failures,rawMilliseconds:times}));
} finally {handle.close();input.destroy();output.destroy();await rm(directory,{recursive:true,force:true})}
