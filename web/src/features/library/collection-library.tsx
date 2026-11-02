import { useEffect, useState } from "react";
import { Link } from "react-router";
import { FolderOpen, LoaderCircle, FileVideo } from "lucide-react";
import { PillSelect } from "../../components/ui/pill-select";
import { libraryRequest } from "./api";
import type { Collection, CollectionMember } from "./api";

type CollectionDetail = { collection: Collection; groups: CollectionMember[]; missing_buckets: string[]; summary?: {video_count: number; total_bytes: number; unknown_size_count: number} };
export function CollectionLibrary({ id, manage }: { id: string; manage: boolean }) {
  const [detail, setDetail] = useState<CollectionDetail>();
  const [error, setError] = useState("");
  const [kind, setKind] = useState("all");
  useEffect(() => {
    const controller = new AbortController();
    let generation = 0;
    const load = async () => {
      const current = ++generation;
      try { const value = await libraryRequest<CollectionDetail>(`/api/v1/library/collections/${id}`, undefined, "GET", controller.signal); if (!controller.signal.aborted && current === generation) { setDetail(value); setError(""); } }
      catch (cause) { if (!controller.signal.aborted && current === generation) setError((cause as Error).message); }
    };
    void load();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const refresh = () => { if (!document.hidden && !timer) timer = setTimeout(() => { timer = undefined; void load(); }, 1500); };
    window.addEventListener("tgstrm:library-update", refresh);
    document.addEventListener("visibilitychange", refresh);
    return () => { controller.abort(); if (timer) clearTimeout(timer); window.removeEventListener("tgstrm:library-update", refresh); document.removeEventListener("visibilitychange", refresh); };
  }, [id]);
  if (!detail) return <section className="topics-empty" role={error ? "alert" : "status"}>{error || <><LoaderCircle className="spin" size={20} /><p>Cargando biblioteca…</p></>}</section>;
  const summary = detail.summary;
  const bytes = summary?.total_bytes ?? 0;
  const unit = bytes > 0 ? Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), 4) : 0;
  const size = `${(bytes / 1024 ** unit).toLocaleString("es", {maximumFractionDigits: 2})} ${["B", "KB", "MB", "GB", "TB"][unit]}`;
  const groups = detail.groups.filter(group => kind === "all" || group.media_kind === kind);
  return <section className="collection-library">
    <header className="collection-heading"><div><p className="eyebrow">BIBLIOTECA</p><h2>{detail.collection.name}</h2></div><button className="catalog-summary collection-summary" type="button" aria-label="Resumen global de la biblioteca">
      <span className="catalog-summary-main"><FileVideo size={14}/><strong>{summary ? `${summary.video_count.toLocaleString("es")} vídeos` : "Resumen pendiente"}</strong></span>
      <span className="catalog-summary-extra">· {summary ? size : "…"}</span>
      <span className="catalog-summary-detail">Películas y series · {summary ? size : "Disponible al reiniciar la API"}{summary && summary.unknown_size_count > 0 ? ` · ${summary.unknown_size_count.toLocaleString("es")} sin tamaño conocido` : ""}</span>
    </button><PillSelect label="Contenido de la carpeta" value={kind} onChange={setKind} options={[{ value: "all", label: "Todos los cajones" }, { value: "series", label: "Series" }, { value: "movies", label: "Películas" }]} /></header>
    {error && <p role="alert">{error}</p>}
    {detail.missing_buckets.length > 0 && <details className="collection-coverage"><summary>{detail.groups.length} cajones · {detail.missing_buckets.length} letras pendientes</summary><p>{detail.missing_buckets.join(" · ")}</p></details>}
    {groups.length ? <div className="groups-grid">{groups.map(group => group.state === "destination_missing" ? <article className="group-card collection-unavailable" key={group.id}><CabinetAvatar collectionID={id} memberID={group.id} /><span className="group-copy"><strong>{group.title}</strong><small>Destino eliminado · requiere revisión</small></span></article> : <Link key={group.id} className="group-card" to={`/library/collections/${id}/groups/${group.group_id}${manage ? "/manage" : ""}`}><CabinetAvatar collectionID={id} memberID={group.id} /><span className="group-copy"><strong>{group.title}</strong><small>{group.media_kind === "series" ? `Series · ${group.bucket}` : "Películas"}</small></span></Link>)}</div> : <div className="topics-empty"><FolderOpen size={22} /><h2>{detail.groups.length ? "No hay cajones con este filtro" : "Tu carpeta está preparada"}</h2><p>Selecciona grupos externos desde Inicio, en Gestión, para revisar y replicar sus topics.</p><Link className="back-link" to="/home">Elegir grupos</Link></div>}
  </section>;
}

function CabinetAvatar({ collectionID, memberID }: { collectionID: string; memberID: string }) {
  const [loaded, setLoaded] = useState(false);
  return <span className="group-mark cabinet-avatar" aria-hidden="true">
    {!loaded && <FolderOpen size={19} />}
    <img src={`/api/v1/library/collections/${collectionID}/groups/${memberID}/artwork`} alt="" loading="lazy" onLoad={() => setLoaded(true)} className={loaded ? "is-loaded" : ""} />
  </span>;
}
