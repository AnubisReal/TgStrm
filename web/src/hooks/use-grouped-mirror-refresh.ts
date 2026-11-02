import {useCallback, useEffect, useRef} from "react";

// Coalesce progress bursts without postponing refresh indefinitely. One request
// per panel can be active; a dirty flag retains updates received during it.
export function useGroupedMirrorRefresh(load:(signal:AbortSignal)=>Promise<void>) {
 const loadRef=useRef(load);loadRef.current=load;
 const refreshRef=useRef<()=>Promise<void>>(async()=>{});
 const refresh=useCallback(()=>refreshRef.current(),[]);
 useEffect(()=>{
  let disposed=false,dirty=false,timer:ReturnType<typeof setTimeout>|undefined;
  let active:Promise<void>|undefined,controller:AbortController|undefined;
  const schedule=()=>{if(disposed)return;dirty=true;if(!timer&&!active)timer=setTimeout(()=>{timer=undefined;void run()},1500)};
  const run=():Promise<void>=>{
   if(disposed)return Promise.resolve();
   if(active){return active.then(()=>run())}
   if(timer){clearTimeout(timer);timer=undefined}
   dirty=false;controller=new AbortController();const signal=controller.signal;
   active=Promise.resolve().then(()=>disposed?undefined:loadRef.current(signal)).catch(()=>{}).finally(()=>{active=undefined;if(dirty&&!disposed&&document.visibilityState==="visible")schedule()});
   return active;
  };
  refreshRef.current=run;
  const visible=()=>{if(document.visibilityState==="visible")schedule()};
  const progress=()=>{dirty=true;if(document.visibilityState==="visible")schedule()};
  const poll=setInterval(()=>{if(document.visibilityState==="visible")schedule()},10000);
  window.addEventListener("tgstrm:mirror-progress",progress);
  document.addEventListener("visibilitychange",visible);
  void run();
  return()=>{disposed=true;controller?.abort();if(timer)clearTimeout(timer);clearInterval(poll);window.removeEventListener("tgstrm:mirror-progress",progress);document.removeEventListener("visibilitychange",visible);refreshRef.current=async()=>{}};
 },[]);
 return refresh;
}
