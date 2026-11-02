import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import vm from 'node:vm';
import ts from 'typescript';
const source=await readFile(new URL('../src/hooks/use-grouped-mirror-refresh.ts',import.meta.url),'utf8');
const compiled=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020}}).outputText;
const flush=async()=>{for(let i=0;i<12;i++)await Promise.resolve()};
function mount(load){
 let cleanup,id=0;const timers=new Map();const win=new EventTarget();const doc=new EventTarget();doc.visibilityState='visible';const exports={};
 vm.runInNewContext(compiled,{exports,require:()=>({useRef:current=>({current}),useCallback:fn=>fn,useEffect:fn=>{cleanup=fn()}}),AbortController,window:win,document:doc,setTimeout:fn=>{timers.set(++id,fn);return id},clearTimeout:id=>timers.delete(id),setInterval:()=>999,clearInterval:()=>{}});
 const refresh=exports.useGroupedMirrorRefresh(load);
 return {refresh,doc,event:()=>win.dispatchEvent(new Event('tgstrm:mirror-progress')),tick:()=>{const pending=[...timers.values()];timers.clear();pending.forEach(fn=>fn())},close:()=>cleanup(),timers};
}
test('bursts share one refresh and continuous events retain the last update',async()=>{
 let calls=0,finish;const h=mount(()=>{calls++;return new Promise(resolve=>{finish=resolve})});await flush();assert.equal(calls,1);
 for(let i=0;i<100;i++)h.event();assert.equal(h.timers.size,0);finish();await flush();assert.equal(h.timers.size,1);
 for(let i=0;i<100;i++)h.event();h.tick();await flush();assert.equal(calls,2);h.event();finish();await flush();h.tick();await flush();assert.equal(calls,3);h.close();finish();await flush();assert.equal(h.timers.size,0);
});
test('hidden view waits for visibility and unmount aborts pending work',async()=>{
 let calls=0,signal;const h=mount(async value=>{calls++;signal=value});await flush();h.doc.visibilityState='hidden';h.event();assert.equal(h.timers.size,0);
 h.doc.visibilityState='visible';h.doc.dispatchEvent(new Event('visibilitychange'));h.tick();await flush();assert.equal(calls,2);h.close();assert.equal(signal.aborted,true);h.event();assert.equal(h.timers.size,0);
});
test('explicit reload waits for an in-flight request then fetches fresh state',async()=>{
 let calls=0,finish;const h=mount(()=>{calls++;return new Promise(resolve=>{finish=resolve})});await flush();let done=false;const reload=h.refresh().then(()=>{done=true});finish();await flush();assert.equal(calls,2);assert.equal(done,false);finish();await reload;assert.equal(done,true);h.close();
});
