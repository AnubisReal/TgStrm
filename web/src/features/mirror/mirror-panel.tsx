import { useEffect, useState } from "react";
import { ArrowLeft, ArrowRight, CheckCircle2, ChevronRight, FolderOpen, History, Link2, LoaderCircle, MessageCircle, Pause, Play, Search, Trash2, X } from "lucide-react";

type Group = { id: string; title: string; forum: boolean; active: boolean };
type Topic = { id: string; title: string };
type MirrorLink = { id:string; source_group_id:string; source_group:string; source_topic_id:string; source_topic:string; destination_group_id:string; destination_group:string; destination_topic_id:string; destination_topic:string; status:"active"|"paused"|"blocked"; synchronized:number; pending:number; failed:number; last_error?:string; last_mirrored_at?:string; history_mode:"new_only"|"include_existing"; history_status:"not_requested"|"pending"|"indexing"|"copying"|"completed"|"failed"; history_total:number; history_completed:number; history_error?:string };

export function MirrorPanel({ manage }: { manage: boolean }) {
  const [links,setLinks]=useState<MirrorLink[]>([]);
  const [groups,setGroups]=useState<Group[]>([]);
  const [sourceGroup,setSourceGroup]=useState("");
  const [sourceTopic,setSourceTopic]=useState("");
  const [destinationTopic,setDestinationTopic]=useState("");
  const [historyMode,setHistoryMode]=useState<"new_only"|"include_existing">("new_only");
  const [sourceTopics,setSourceTopics]=useState<Topic[]>([]);
  const [destinationTopics,setDestinationTopics]=useState<Topic[]>([]);
  const [picker,setPicker]=useState<"source"|"destination"|null>(null);
  const [pickerGroup,setPickerGroup]=useState("");
  const [pickerQuery,setPickerQuery]=useState("");
  const [topicsLoading,setTopicsLoading]=useState(false);
  const [state,setState]=useState<"loading"|"ready"|"saving">("loading");
  const [error,setError]=useState("");
  const primary=groups.find((group)=>group.active);

  const reload=()=>fetch("/api/v1/mirrors",{credentials:"same-origin"}).then(async response=>{if(!response.ok) throw new Error(); return response.json() as Promise<{links:MirrorLink[]}>}).then(data=>{setLinks(data.links);setState("ready")}).catch(()=>{setError("No se pudieron cargar los vínculos.");setState("ready")});
  useEffect(()=>{void reload(); fetch("/api/v1/groups",{credentials:"same-origin"}).then(r=>r.json() as Promise<{groups:Group[]}>).then(data=>setGroups(data.groups));},[]);
  useEffect(()=>{if(!primary){setDestinationTopics([]);return} fetch(`/api/v1/groups/${primary.id}/topics?refresh=0`,{credentials:"same-origin"}).then(r=>r.json() as Promise<{topics:Topic[]}>).then(data=>setDestinationTopics(data.topics));},[primary?.id]);
  useEffect(()=>{const timer=window.setInterval(()=>void reload(),5000);return()=>window.clearInterval(timer)},[]);
  useEffect(()=>{const refresh=()=>void reload();window.addEventListener("tgstrm:mirror-progress",refresh);return()=>window.removeEventListener("tgstrm:mirror-progress",refresh)},[]);
  useEffect(()=>{if(!picker)return;const close=(event:KeyboardEvent)=>{if(event.key==="Escape")setPicker(null)};window.addEventListener("keydown",close);return()=>window.removeEventListener("keydown",close)},[picker]);

  const openSource=()=>{setPicker("source");setPickerGroup("");setPickerQuery("")};
  const openDestination=()=>{setPicker("destination");setPickerGroup(primary?.id||"");setPickerQuery("")};
  const enterSourceGroup=async(groupID:string)=>{setPickerGroup(groupID);setPickerQuery("");setTopicsLoading(true);setSourceTopics([]);try{const response=await fetch(`/api/v1/groups/${groupID}/topics?refresh=0`,{credentials:"same-origin"});const data=await response.json() as {topics:Topic[]};setSourceTopics(data.topics)}finally{setTopicsLoading(false)}};
  const selectedSourceGroup=groups.find(group=>group.id===sourceGroup);
  const selectedSourceTopic=sourceTopics.find(topic=>topic.id===sourceTopic);
  const selectedDestinationTopic=destinationTopics.find(topic=>topic.id===destinationTopic);
  const normalizedQuery=pickerQuery.trim().toLocaleLowerCase("es");
  const visiblePickerGroups=groups.filter(group=>group.forum&&!group.active&&(!normalizedQuery||group.title.toLocaleLowerCase("es").includes(normalizedQuery)));
  const pickerTopics=(picker==="destination"?destinationTopics:sourceTopics).filter(topic=>!normalizedQuery||topic.title.toLocaleLowerCase("es").includes(normalizedQuery));

  const create=async()=>{setState("saving");setError("");const response=await fetch("/api/v1/mirrors",{method:"POST",credentials:"same-origin",headers:{"Content-Type":"application/json"},body:JSON.stringify({source_topic_id:sourceTopic,destination_topic_id:destinationTopic,history_mode:historyMode})});if(!response.ok){const body=await response.json().catch(()=>({message:"No se pudo crear el vínculo."}));setError(body.message);setState("ready");return}setSourceTopic("");setDestinationTopic("");setHistoryMode("new_only");await reload()};
  const change=async(link:MirrorLink,status:"active"|"paused")=>{await fetch(`/api/v1/mirrors/${link.id}`,{method:"PATCH",credentials:"same-origin",headers:{"Content-Type":"application/json"},body:JSON.stringify({status})});await reload()};
  const remove=async(link:MirrorLink)=>{await fetch(`/api/v1/mirrors/${link.id}`,{method:"DELETE",credentials:"same-origin"});await reload()};

  return <section className="mirror-view">
    {manage && <div className="mirror-builder">
      <header><span className="mirror-icon"><Link2 size={18}/></span><div><strong>Nuevo vínculo</strong><small>Elige el origen, el destino y desde qué momento quieres sincronizar.</small></div></header>
      <div className="mirror-fields">
        <label><span>Origen externo</span><button type="button" className="mirror-picker-trigger" onClick={openSource}><span>{selectedSourceTopic?<><small>{selectedSourceGroup?.title}</small><strong>{selectedSourceTopic.title}</strong></>:<strong>Buscar grupo y topic</strong>}</span><ChevronRight size={15}/></button></label>
        <ArrowRight className="mirror-arrow" size={18}/>
        <label><span>Destino en {primary?.title||"tu grupo principal"}</span><button type="button" className="mirror-picker-trigger" disabled={!primary} onClick={openDestination}><span>{selectedDestinationTopic?<><small>{primary?.title}</small><strong>{selectedDestinationTopic.title}</strong></>:<strong>Buscar topic de destino</strong>}</span><ChevronRight size={15}/></button></label>
        <button className="mirror-create" disabled={!sourceTopic||!destinationTopic||state==="saving"} onClick={create}>{state==="saving"?<LoaderCircle className="spin" size={15}/>:<Link2 size={15}/>} Vincular</button>
      </div>
      <div className="mirror-history-choice" role="group" aria-label="Archivos iniciales del espejo"><button type="button" className={historyMode==="new_only"?"selected":""} onClick={()=>setHistoryMode("new_only")}><ArrowRight size={14}/><span><strong>Solo nuevos</strong><small>Empieza a sincronizar desde ahora</small></span></button><button type="button" className={historyMode==="include_existing"?"selected":""} onClick={()=>setHistoryMode("include_existing")}><History size={14}/><span><strong>Incluir existentes</strong><small>Prepara el historial y después continúa en tiempo real</small></span></button></div>
      {error&&<p className="mirror-error">{error}</p>}
    </div>}
    {picker&&<div className="mirror-picker-backdrop" role="presentation" onMouseDown={()=>setPicker(null)}><section className="mirror-picker" role="dialog" aria-modal="true" aria-labelledby="mirror-picker-title" onMouseDown={event=>event.stopPropagation()}>
      <header>{picker==="source"&&pickerGroup?<button type="button" onClick={()=>{setPickerGroup("");setPickerQuery("")}} aria-label="Volver a grupos"><ArrowLeft size={17}/></button>:<span className="mirror-picker-mark">{picker==="source"?<FolderOpen size={17}/>:<Link2 size={17}/>}</span>}<div><p className="eyebrow">{picker==="source"&&pickerGroup?"TOPICS DEL GRUPO":picker==="source"?"ORIGEN EXTERNO":"GRUPO PRINCIPAL"}</p><h2 id="mirror-picker-title">{picker==="source"&&pickerGroup?groups.find(group=>group.id===pickerGroup)?.title:picker==="source"?"Seleccionar grupo":"Seleccionar destino"}</h2></div><button type="button" onClick={()=>setPicker(null)} aria-label="Cerrar"><X size={17}/></button></header>
      <label className="mirror-picker-search"><Search size={16}/><input autoFocus value={pickerQuery} onChange={event=>setPickerQuery(event.target.value)} placeholder={picker==="source"&&!pickerGroup?"Buscar grupos":"Buscar topics"}/>{pickerQuery&&<button type="button" onClick={()=>setPickerQuery("")} aria-label="Limpiar búsqueda"><X size={14}/></button>}</label>
      <div className="mirror-picker-list">{picker==="source"&&!pickerGroup?visiblePickerGroups.map(group=><button type="button" key={group.id} onClick={()=>void enterSourceGroup(group.id)}><span className="mirror-result-icon"><FolderOpen size={17}/></span><span><strong>{group.title}</strong><small>Explorar sus topics</small></span><ChevronRight size={16}/></button>):topicsLoading?<div className="mirror-picker-empty"><LoaderCircle className="spin" size={20}/> Cargando topics…</div>:pickerTopics.map(topic=><button type="button" key={topic.id} className={(picker==="source"?sourceTopic:destinationTopic)===topic.id?"selected":""} onClick={()=>{if(picker==="source"){setSourceGroup(pickerGroup);setSourceTopic(topic.id)}else setDestinationTopic(topic.id);setPicker(null)}}><span className="mirror-result-icon"><MessageCircle size={17}/></span><span><strong>{topic.title}</strong><small>{picker==="source"?"Topic externo":primary?.title}</small></span>{(picker==="source"?sourceTopic:destinationTopic)===topic.id?<CheckCircle2 size={17}/>:<ChevronRight size={16}/>}</button>)}</div>
      {!topicsLoading&&((picker==="source"&&!pickerGroup&&!visiblePickerGroups.length)||((picker!=="source"||pickerGroup)&&!pickerTopics.length))&&<div className="mirror-picker-empty"><Search size={20}/><strong>Sin resultados</strong><small>Prueba con otro nombre.</small></div>}
    </section></div>}
    <div className="mirror-heading"><div><p className="eyebrow">SINCRONIZACIÓN</p><h3>Topics vinculados</h3></div><span>{links.filter(link=>link.status==="active").length} activos</span></div>
    {state==="loading"?<div className="topics-empty"><LoaderCircle className="spin" size={22}/><h2>Cargando vínculos</h2></div>:links.length?<div className="mirror-grid">{links.map(link=><article className="mirror-card" key={link.id}>
      <div className="mirror-pair"><span><small>{link.source_group}</small><strong>{link.source_topic}</strong></span><i><ArrowRight size={15}/></i><span><small>{link.destination_group}</small><strong>{link.destination_topic}</strong></span></div>
      <div className="mirror-meta"><span className={`mirror-status ${link.status}`}>{link.status==="active"?<CheckCircle2 size={13}/>:<Pause size={13}/>} {link.status==="active"?"En tiempo real":link.status==="paused"?"Pausado":"Requiere atención"}</span><span>{link.synchronized} sincronizados</span>{link.history_mode==="include_existing"&&link.history_status!=="completed"&&<span><History size={12}/> {link.history_status==="pending"?"Historial en cola":link.history_status==="indexing"?"Leyendo historial":`${link.history_completed} de ${link.history_total} históricos`}</span>}{link.pending>0&&<span>{link.pending} pendientes</span>}{link.failed>0&&<span className="failed">{link.failed} con error</span>}</div>
      {(link.last_error||link.history_error)&&<p className="mirror-error">{link.history_error||link.last_error}</p>}
      {manage&&<footer><button onClick={()=>change(link,link.status==="active"?"paused":"active")}>{link.status==="active"?<Pause size={14}/>:<Play size={14}/>} {link.status==="active"?"Pausar":"Activar"}</button><button className="danger" onClick={()=>remove(link)}><Trash2 size={14}/> Eliminar vínculo</button></footer>}
    </article>)}</div>:<div className="topics-empty"><span className="empty-icon"><Link2 size={22}/></span><h2>Todavía no hay topics vinculados</h2><p>Abre la gestión del espejo para crear el primer vínculo.</p></div>}
  </section>
}
