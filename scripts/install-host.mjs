#!/usr/bin/env node
// Install only t3oxide's files. Never install/update/start T3, Node, SSH, or a VPN.
import { readFile, writeFile, mkdir, copyFile, rename, symlink, lstat, rm } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { dirname, resolve, join } from 'node:path';
import { homedir } from 'node:os';
import { readConfig, T3 } from '../adapter/t3.mjs';
import { Fault, requireThat } from '../adapter/core.mjs';
const args=process.argv.slice(2), option=name=>{const i=args.indexOf(name);return i<0?undefined:args[i+1];};
const root=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const base=join(homedir(),'.local/share/t3oxide'), configFile=join(homedir(),'.config/t3oxide/host.json');
async function main(){
  const [major,minor]=process.versions.node.split('.').map(Number);
  requireThat(major>22||major===22&&minor>=4,'Use an existing Node 22.4+ binary. No runtime was installed.');
  let config;
  try {config=JSON.parse(await readFile(configFile,'utf8'));} catch(e){if(e.code!=='ENOENT')throw e;}
  if(!config || option('--base-url') || option('--token-file')){
    const url=new URL(option('--base-url')??config?.baseUrl??'http://127.0.0.1:3773');
    requireThat(['http:','https:'].includes(url.protocol)&&['127.0.0.1','localhost','[::1]'].includes(url.hostname)&&url.pathname==='/'&&!url.username&&!url.password&&!url.search&&!url.hash,'Supply the existing loopback T3 base URL.');
    const tokenFile=option('--token-file')??config?.tokenFile;
    requireThat(tokenFile?.startsWith('/'),'Supply --token-file with a private host-local token path. Token values never go on the command line.');
    const response=await fetch(new URL('/.well-known/t3/environment',url),{signal:AbortSignal.timeout(10000),redirect:'error'});
    requireThat(response.ok,'Existing local T3 descriptor was unavailable. No server was started.');
    const d=await response.json();requireThat(d.orchestrationProtocolVersion===2&&typeof d.environmentId==='string','This source adapter requires T3 protocol 2.');
    requireThat(!config||config.environmentId===d.environmentId,'Existing t3oxide identity differs; do not silently rebind this host configuration.');
    config={baseUrl:url.href,environmentId:d.environmentId,tokenFile,allowWrites:config?.allowWrites===true};
  }
  if(args.includes('--enable-writes'))config.allowWrites=true;
  if(args.includes('--read-only'))config.allowWrites=false;
  await mkdir(dirname(configFile),{recursive:true,mode:0o700});
  const staged=configFile+'.new';await writeFile(staged,JSON.stringify(config,null,2)+'\n',{mode:0o600});
  let verified;
  try {verified=await readConfig(staged);const backend=new T3(verified,()=>{});try{await backend.probe();await backend.http('/api/orchestration/shell');}finally{backend.close();}}
  catch(e){await rm(staged,{force:true});throw e;}
  const files=['core.mjs','wire.mjs','journal.mjs','t3.mjs','main.mjs'],hash=createHash('sha256');
  for(const name of files)hash.update(await readFile(join(root,'adapter',name)));
  const revision=hash.digest('hex').slice(0,16),release=join(base,'releases',revision);
  await mkdir(release,{recursive:true,mode:0o700});
  for(const name of files)await copyFile(join(root,'adapter',name),join(release,name));
  await copyFile(join(root,'NOTICE'),join(release,'NOTICE'));
  const current=join(base,'current');
  try {requireThat((await lstat(current)).isSymbolicLink(),'The install current path is not a symlink; preserve it and resolve manually.');}catch(e){if(e.code!=='ENOENT')throw e;}
  const link=join(base,`.next-${process.pid}`);await symlink(release,link);await rename(link,current);await rename(staged,configFile);
  console.log(JSON.stringify({adapterRevision:revision,environmentId:verified.environmentId,node:process.execPath,adapter:join(current,'main.mjs'),hostConfig:configFile,writesEnabled:config.allowWrites===true},null,2));
}
main().catch(e=>{console.error(`t3oxide install: ${e instanceof Fault?e.message:'Installation failed; inspect host-local paths and permissions.'}`);process.exitCode=1;});
