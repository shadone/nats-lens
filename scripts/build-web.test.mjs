import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,mkdir,writeFile,readFile,rm,stat} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {buildWeb} from './build-web.mjs';

async function fixture(t) {
 const root=await mkdtemp(join(tmpdir(),'nats-web-build-'));
 t.after(()=>rm(root,{recursive:true,force:true}));
 for(const dir of ['web/src/gen','web/public','web/node_modules','internal/webui/dist','proto/natslens/v1'])await mkdir(join(root,dir),{recursive:true});
 for(const [name,value] of Object.entries({'web/src/main.tsx':'initial','web/package.json':'{}','web/package-lock.json':'{}','web/node_modules/.package-lock.json':'{}','web/vite.config.ts':'config','web/tsconfig.json':'{}','web/.npmrc':'config','proto/natslens/v1/example.proto':'schema'}))await writeFile(join(root,name),value);
 let calls=0;
 const build=async out=>{calls++;await mkdir(join(out,'assets'),{recursive:true});await writeFile(join(out,'index.html'),'<script src="/assets/app.js"></script>');await writeFile(join(out,'assets/app.js'),String(calls));};
 return {root,build,calls:()=>calls};
}
test('unchanged inputs and verified outputs reuse assets without touching them',async t=>{
 const f=await fixture(t);await buildWeb(f);const path=join(f.root,'internal/webui/dist/assets/app.js'),before=await stat(path);
 assert.equal((await buildWeb(f)).built,false);assert.equal(f.calls(),1);assert.equal((await stat(path)).mtimeMs,before.mtimeMs);
});
for(const file of ['web/src/main.tsx','web/src/gen/new_pb.ts','web/public/icon.svg','web/package.json','web/package-lock.json','web/node_modules/.package-lock.json','web/vite.config.ts','web/tsconfig.json','web/.npmrc','proto/natslens/v1/example.proto'])test(`changes to ${file} invalidate assets`,async t=>{
 const f=await fixture(t);await buildWeb(f);await writeFile(join(f.root,file),'changed');assert.equal((await buildWeb(f)).built,true);assert.equal(f.calls(),2);
});
test('deletions, corrupt state and missing or edited outputs rebuild',async t=>{
 const f=await fixture(t);await writeFile(join(f.root,'web/src/removed.ts'),'source');await buildWeb(f);
 await rm(join(f.root,'web/src/removed.ts'));assert.equal((await buildWeb(f)).built,true);
 await rm(join(f.root,'internal/webui/dist/assets/app.js'));assert.equal((await buildWeb(f)).built,true);
 await writeFile(join(f.root,'internal/webui/dist/assets/app.js'),'tampered');assert.equal((await buildWeb(f)).built,true);
 await writeFile(join(f.root,'internal/webui/.build-state.json'),'not json');assert.equal((await buildWeb(f)).built,true);
});
test('Go-only changes reuse UI; force and environment changes do not',async t=>{
 const f=await fixture(t);await buildWeb({...f,environment:{MODE:'a'}});
 await writeFile(join(f.root,'backend.go'),'package example');assert.equal((await buildWeb({...f,environment:{MODE:'a'}})).built,false);
 assert.equal((await buildWeb({...f,environment:{MODE:'b'}})).built,true);
 assert.equal((await buildWeb({...f,environment:{MODE:'b'},force:true})).built,true);
});
test('failed builds and source changes during a build preserve the prior UI',async t=>{
 const f=await fixture(t);await buildWeb(f);const path=join(f.root,'internal/webui/dist/assets/app.js'),before=await readFile(path,'utf8');
 await writeFile(join(f.root,'web/src/main.tsx'),'changed');
 await assert.rejects(buildWeb({...f,build:async out=>{await f.build(out);throw Error('synthetic build failure');}}),/synthetic/);assert.equal(await readFile(path,'utf8'),before);
 await assert.rejects(buildWeb({...f,build:async out=>{await f.build(out);await writeFile(join(f.root,'web/src/main.tsx'),'changed during build');}}),/changed during/);assert.equal(await readFile(path,'utf8'),before);
});
test('missing installed-dependency marker is an error, never a cache hit',async t=>{
 const f=await fixture(t);await buildWeb(f);await rm(join(f.root,'web/node_modules/.package-lock.json'));
 await assert.rejects(buildWeb(f),/dependencies/);
});
