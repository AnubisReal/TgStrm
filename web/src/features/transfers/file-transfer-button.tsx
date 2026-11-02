import { createPortal } from "react-dom";
import { useEffect, useRef, useState } from "react";
import { Send, X, LoaderCircle } from "lucide-react";
import { PillSelect } from "../../components/ui/pill-select";

type TargetGroup = { id: string; title: string; forum: boolean };
type TargetTopic = { id: string; title: string; hidden?: boolean; closed?: boolean; telegram_id: number };
type Progress = { status: string; completed: number; requested: number; last_error?: string; next_attempt_at?: string };

export function FileTransferButton({ groupId, topicId, selected }: { groupId: string; topicId: string; selected?: string[] }) {
 const [open,setOpen]=useState(false),[groups,setGroups]=useState<TargetGroup[]>([]),[topics,setTopics]=useState<TargetTopic[]>([]);
 const [target,setTarget]=useState(""),[topic,setTopic]=useState(""),[query,setQuery]=useState(""),[topicQuery,setTopicQuery]=useState("");
 const [loading,setLoading]=useState(false),[sending,setSending]=useState(false),[error,setError]=useState(""),[progress,setProgress]=useState<Progress>();
 const key=`tgstrm.file-transfer:${groupId}:${topicId}`;
 const [job,setJob]=useState(()=>{try{return localStorage.getItem(key)||""}catch{return ""}});
 const dialog=useRef<HTMLElement>(null),trigger=useRef<HTMLButtonElement>(null);
 const destination=groups.find(group=>group.id===target);
 useEffect(()=>{
  if(!open)return;
  const controller=new AbortController();setLoading(true);setError("");
  fetch("/api/v1/file-transfers/targets",{credentials:"same-origin",signal:controller.signal}).then(async response=>{const data=await response.json();if(!response.ok)throw Error(data.message);setGroups(data.groups||[])}).catch(e=>{if(!controller.signal.aborted)setError(e.message)}).finally(()=>{if(!controller.signal.aborted)setLoading(false)});
  const previous=document.activeElement as HTMLElement|null;dialog.current?.querySelector<HTMLButtonElement>("button")?.focus();
  const onKey=(event:KeyboardEvent)=>{if(event.key==="Escape"){setOpen(false);return}if(event.key!=="Tab")return;const controls=[...(dialog.current?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),[tabindex="0"]')||[])];const first=controls[0],last=controls.at(-1);if(event.shiftKey&&document.activeElement===first){event.preventDefault();last?.focus()}else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first?.focus()}};
  document.addEventListener("keydown",onKey);return()=>{controller.abort();document.removeEventListener("keydown",onKey);previous?.focus()};
 },[open]);
 useEffect(()=>{
  setTopics([]);setTopic("");setTopicQuery("");if(!open||!destination?.forum)return;
  const controller=new AbortController();setLoading(true);setError("");
  fetch(`/api/v1/groups/${encodeURIComponent(target)}/topics`,{credentials:"same-origin",signal:controller.signal}).then(async response=>{const data=await response.json();if(!response.ok)throw Error(data.message);setTopics(data.topics||[])}).catch(e=>{if(!controller.signal.aborted)setError(e.message)}).finally(()=>{if(!controller.signal.aborted)setLoading(false)});
  return()=>controller.abort();
 },[open,target,destination?.forum]);
 useEffect(()=>{
  if(!job)return;let stopped=false;let timer:ReturnType<typeof setTimeout>;
  const check=async()=>{try{const response=await fetch(`/api/v1/file-transfers/${encodeURIComponent(job)}`,{credentials:"same-origin"});const data=await response.json();if(stopped)return;if(!response.ok)throw Error(data.message);setProgress(data);if(data.status==="pending"||data.status==="running")timer=setTimeout(check,3000)}catch(e){if(!stopped){setError((e as Error).message);timer=setTimeout(check,10000)}}};void check();return()=>{stopped=true;clearTimeout(timer)};
 },[job]);
 const busy=sending||progress?.status==="running"||progress?.status==="pending"||progress?.status==="attention";
 const cancel=async()=>{setError("");try{const response=await fetch(`/api/v1/file-transfers/${encodeURIComponent(job)}/cancel`,{method:"POST",credentials:"same-origin"});const data=await response.json();if(!response.ok)throw Error(data.message);setProgress(current=>current?{...current,status:"cancelled",last_error:undefined}:current)}catch(e){setError((e as Error).message)}};
 const send=async()=>{
  setSending(true);setError("");try{const response=await fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/topics/${encodeURIComponent(topicId)}/file-transfers`,{method:"POST",credentials:"same-origin",headers:{"Content-Type":"application/json"},body:JSON.stringify({destination_group_id:target,destination_topic_id:destination?.forum?topic:"",message_ids:selected||[],all:!selected})});const data=await response.json();if(!response.ok)throw Error(data.message||"No se pudo iniciar el envío.");try{localStorage.setItem(key,data.id)}catch{/* Progress remains available in Activity. */}setJob(data.id);setProgress({status:"pending",completed:0,requested:data.requested});setOpen(false)}catch(e){setError((e as Error).message)}finally{setSending(false)}
 };
 return <>
  <button ref={trigger} type="button" onClick={()=>setOpen(true)} disabled={!!selected&&!selected.length} title="Copiar sin asociar" aria-label="Copiar sin asociar"><Send size={14}/><span className="mobile-action-label">Copiar sin asociar</span></button>
  {progress&&<span className="file-transfer-progress" role="status" title={progress.last_error}>{progress.status==="cancelled"?"Envío detenido":progress.status==="completed"?`Enviados ${progress.completed}`:progress.status==="attention"?"Envío requiere revisión":`${progress.completed}/${progress.requested} · ${progress.last_error?"Esperando a Telegram":"Enviando"}`}</span>}
  {open&&createPortal(<div className="episode-resolution-backdrop" onMouseDown={event=>{if(event.target===event.currentTarget)setOpen(false)}}>
   <section ref={dialog} className="episode-resolution topic-match-dialog file-transfer-dialog" role="dialog" aria-modal="true" aria-labelledby="file-transfer-title">
    <header><div><p className="eyebrow">COPIAR ARCHIVOS</p><h2 id="file-transfer-title">Enviar a otro grupo</h2></div><button className="tmdb-close" aria-label="Cerrar" onClick={()=>setOpen(false)}><X size={17}/></button></header>
    <p>{selected?`${selected.length} vídeos seleccionados` : "Todos los vídeos del origen, incluidos los enviados como documentos."} Se conservan sus pies de archivo. El destino se elige para este envío.</p>
    <div className="file-transfer-fields">
     <label>Buscar grupo<input type="search" value={query} onChange={event=>setQuery(event.target.value)} placeholder="Nombre del grupo…"/></label>
     <PillSelect label="Grupo destino" value={target} options={[{value:"",label:"Selecciona un grupo"},...groups.filter(group=>(group.id!==groupId||group.forum)&&(group.id===target||group.title.toLocaleLowerCase().includes(query.toLocaleLowerCase()))).map(group=>({value:group.id,label:group.title}))]} onChange={setTarget}/>
     {destination?.forum&&<><label>Buscar topic<input type="search" value={topicQuery} onChange={event=>setTopicQuery(event.target.value)} placeholder="Nombre del topic…"/></label><PillSelect label="Topic destino" value={topic} disabled={loading} options={[{value:"",label:loading?"Cargando topics…":"Selecciona un topic existente"},...topics.filter(item=>item.id!==topicId&&!item.hidden&&!item.closed&&(item.id===topic||item.title.toLocaleLowerCase().includes(topicQuery.toLocaleLowerCase()))).map(item=>({value:item.id,label:item.title}))]} onChange={setTopic}/></>}
    </div>
    {loading&&<p role="status"><LoaderCircle className="spin" size={14}/> Cargando destinos…</p>}
    {error&&<p role="alert" className="jobs-error">{error}</p>}
    {progress?.last_error&&<p role="status">{progress.last_error}</p>}
    {(progress?.status==="pending"||progress?.status==="attention")&&<button className="resolution-cancel" onClick={()=>void cancel()}>Detener este envío</button>}
    {progress?.status==="attention"&&<p>Si Telegram no confirmó el último lote, comprueba el destino antes de detener este trabajo y preparar otra selección.</p>}
    <footer><button className="resolution-cancel" onClick={()=>setOpen(false)}>Cancelar</button><button className="resolution-save" disabled={busy||loading||!destination||(destination.forum&&!topic)} onClick={()=>void send()}>{sending?<LoaderCircle className="spin" size={15}/>:<Send size={15}/>} {sending?"Preparando envío…":busy?"Envío en curso":"Confirmar envío"}</button></footer>
   </section>
  </div>, trigger.current?.closest(".dashboard-page") || document.body)}
 </>;
}
