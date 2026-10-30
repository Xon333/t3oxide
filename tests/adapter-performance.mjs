// Content-free repeatable projection component measurement. Pass either the
// untouched baseline or resulting source root; the workload is identical.
// It measures synchronous flushView only: no transport, terminal or pixels.
import {resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
import {performance} from 'node:perf_hooks';
const source=resolve(process.argv[2] ?? new URL('..',import.meta.url).pathname);
const {T3}=await import(pathToFileURL(resolve(source,'adapter/t3.mjs')).href);
const sampleCount=2000,warmup=100;
const quant=(xs,q)=>xs.slice().sort((a,b)=>a-b)[Math.ceil(xs.length*q)-1];
for(const [n,chars,historical] of [[24,1800,false],[80,1600,true]]) {
  let frameCount=0;
  const backend=new T3({environmentId:'synthetic-env'},()=>frameCount++);
  backend.current={threadId:'synthetic-thread',sequence:0,state:historical?'historical':'live',historical,live:!historical,
    items:Array.from({length:n},(_,i)=>({key:`synthetic-thread/${i}`,sourceThreadId:'synthetic-thread',itemId:String(i),
      kind:'assistant_message',position:i,text:'x'.repeat(chars),streaming:i===n-1,truncated:false,attachmentCount:0})),requests:[]};
  backend.flushView();const bytes=Buffer.byteLength(JSON.stringify(backend.current));
  for(const mode of ['sequence-only','tail-update']) {
    function step(i){backend.current.sequence++;if(mode==='tail-update')backend.current.items.at(-1).text='x'.repeat(chars-8)+String(i).padStart(8,'0');backend.flushView()}
    for(let i=0;i<warmup;i++)step(i);
    const memoryBefore=process.memoryUsage(),times=[];
    for(let i=0;i<sampleCount;i++){const start=performance.now();step(i);times.push(performance.now()-start)}
    const memoryAfter=process.memoryUsage();
    let calls=0,encodedChars=0;const original=JSON.stringify;
    JSON.stringify=(...args)=>{calls++;const encoded=original(...args);encodedChars+=encoded?.length??0;return encoded};
    try{step(2222)}finally{JSON.stringify=original}
    console.log(JSON.stringify({schema:1,workload:'bounded-projection-v1',node:process.version,platform:process.platform,arch:process.arch,
      n,mode,bytes,samples:sampleCount,warmup,boundary:'synchronous flushView; warm in-process; no transport/terminal',
      median:quant(times,.5),p95:quant(times,.95),p99:quant(times,.99),worst:Math.max(...times),
      mean:times.reduce((a,b)=>a+b,0)/times.length,instrumentedOneFlush:{stringifyCalls:calls,encodedChars},frameCount,
      memoryBefore,memoryAfter,maxRssKiB:process.resourceUsage().maxRSS,
      memoryBoundary:'process RSS/heap snapshots, not allocated-byte counts',rawMilliseconds:times}));
  }
  backend.close();
}
