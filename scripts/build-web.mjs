// Production assets are reused only when both inputs and every output match.
import {createHash,randomUUID} from 'node:crypto';
import {readFile,writeFile,readdir,stat,lstat,readlink,realpath,mkdir,mkdtemp,rename,rm} from 'node:fs/promises';
import {dirname,join,resolve} from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {spawn} from 'node:child_process';

const VERSION=1;
const ignoredEnvironment=new Set(['_','PWD','OLDPWD','SHLVL','MAKEFLAGS','MAKELEVEL','MFLAGS']);
async function exists(path) {try{await stat(path);return true}catch(e){if(e.code==='ENOENT')return false;throw e}}
async function tree(hash,path,label,ancestors=new Set()) {
 const info=await lstat(path);
 if(info.isSymbolicLink())hash.update(`link:${label}:${await readlink(path)}\0`);
 const target=await stat(path);
 if(target.isDirectory()) {
  const actual=await realpath(path);if(ancestors.has(actual))throw Error(`Cyclic build input: ${label}`);
  const next=new Set([...ancestors,actual]);
  for(const name of (await readdir(path)).sort()) {
   const child=`${label}/${name}`;
   if(child==='web/node_modules'||name==='.DS_Store')continue;
   await tree(hash,join(path,name),child,next);
  }
 } else if(target.isFile()) {
  const bytes=await readFile(path);hash.update(`file:${label}:${bytes.length}\0`);hash.update(bytes);
 } else throw Error(`Unsupported build input: ${label}`);
}
async function inputs(root,environment) {
 const h=createHash('sha256');h.update(JSON.stringify({version:VERSION,node:process.version,environment:Object.entries(environment).filter(([k])=>!ignoredEnvironment.has(k)).sort(([a],[b])=>a.localeCompare(b))}));
 h.update(await readFile(fileURLToPath(import.meta.url)));
 await tree(h,join(root,'web'),'web');
 const marker=join(root,'web/node_modules/.package-lock.json');
 if(!await exists(marker))throw Error('Web dependencies are missing; run make web to install from the lockfile.');
 await tree(h,marker,'installed-dependencies');
 if(await exists(join(root,'proto')))await tree(h,join(root,'proto'),'proto');
 return h.digest('hex');
}
async function output(root) {
 const h=createHash('sha256');await tree(h,root,'dist');return h.digest('hex');
}
async function run(command,args,options) {
 await new Promise((yes,no)=>{
  const child=spawn(command,args,{stdio:'inherit',...options});
  child.once('error',no);child.once('exit',(code,signal)=>code===0?yes():no(Error(`${command} failed (${signal??code})`)));
 });
}
async function productionBuild(root,out) {await run('npm',['run','build'],{cwd:join(root,'web'),env:{...process.env,NATSLENS_WEB_OUT_DIR:out}})}
export async function buildWeb({root,build,force=false,environment=process.env,afterReady}) {
 const home=join(root,'internal/webui'),dist=join(home,'dist'),statePath=join(home,'.build-state.json'),lock=join(home,'.build-lock');
 await mkdir(home,{recursive:true});
 try{await mkdir(lock)}catch(e){if(e.code==='EEXIST')throw Error('Another web build owns internal/webui/.build-lock; if it crashed, verify its owner.json PID is no longer running before removing that lock.');throw e}
 let stage;
 try {
  await writeFile(join(lock,'owner.json'),JSON.stringify({pid:process.pid}));
  const input=await inputs(root,environment);
  let state;try{state=JSON.parse(await readFile(statePath,'utf8'))}catch(e){if(!['ENOENT',undefined].includes(e.code))throw e}
  if(!force&&state?.version===VERSION&&state.input===input&&await exists(join(dist,'index.html'))) {
   let valid=false;try{valid=await output(dist)===state.output}catch(e){if(e.code!=='ENOENT')throw e}
   if(valid){await afterReady?.();return {built:false}}
  }
  stage=await mkdtemp(join(home,'.build-stage-'));
  // npm's standalone command also keeps the placeholder marker available.
  await mkdir(dist,{recursive:true});
  await (build??(out=>productionBuild(root,out)))(stage);
  const index=await stat(join(stage,'index.html'));
  if(!index.isFile()||index.size===0)throw Error('Web build did not produce a nonempty index.html');
  await writeFile(join(stage,'.gitkeep'),'');
  if(await inputs(root,environment)!==input)throw Error('Web inputs changed during the build; assets were not published. Retry with stable inputs.');
  const digest=await output(stage),backup=join(home,`.build-previous-${randomUUID()}`);
  await rename(dist,backup);
  try{await rename(stage,dist);stage=undefined}catch(e){await rename(backup,dist);throw e}
  await rm(backup,{recursive:true,force:true});
  const temp=statePath+'.'+randomUUID();
  try{await writeFile(temp,JSON.stringify({version:VERSION,input,output:digest})+'\n');await rename(temp,statePath)}finally{await rm(temp,{force:true})}
  await afterReady?.();
  return {built:true};
 } finally {
  if(stage)await rm(stage,{recursive:true,force:true});
  await rm(lock,{recursive:true,force:true});
 }
}
if(process.argv[1]&&import.meta.url===pathToFileURL(resolve(process.argv[1])).href) {
 try {
  const args=process.argv.slice(2);let binary;
  for(let i=0;i<args.length;i++){if(args[i]==='--force')continue;if(args[i]==='--go'&&args[i+1])binary=args[++i];else throw Error('Usage: node scripts/build-web.mjs [--force] [--go OUTPUT]');}
  const root=resolve(dirname(fileURLToPath(import.meta.url)),'..');
  const result=await buildWeb({root,force:args.includes('--force'),afterReady:binary?()=>run('go',['build','-o',binary,'./cmd/natslens'],{cwd:root}):undefined});
  console.log(result.built?'web: rebuilt and verified production assets':'web: unchanged inputs and verified assets; skipped frontend build');
 }catch(e){console.error(e.message);process.exitCode=1}
}
