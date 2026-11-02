import { LibraryCleanup } from "./library-cleanup";
import { FolderOpen, LoaderCircle, Save, Plus, Check, RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";
import { PillSelect } from "../../components/ui/pill-select";
import { libraryRequest, libraryRequestKey } from "./api";
import type { Collection, LibraryTarget } from "./api";

type RenameItem = { id: string; previous_title: string; desired_title: string; manual: boolean; conflict: boolean; available: boolean };
export function CollectionSettings({ groups }: { groups: { id: string; title: string; forum: boolean }[] }) {
  const [target, setTarget] = useState<LibraryTarget>();
  const [mode, setMode] = useState("group");
  const [groupID, setGroupID] = useState("");
  const [collectionID, setCollectionID] = useState("");
  const [collections, setCollections] = useState<Collection[]>([]);
  const [folders, setFolders] = useState<{ id: number; title: string; shared: boolean }[]>([]);
  const [folderID, setFolderID] = useState("");
  const [name, setName] = useState("Biblioteca");
  const [series, setSeries] = useState("Series {letra}");
  const [movies, setMovies] = useState("Películas");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [preview, setPreview] = useState<{ revision: number; items: RenameItem[] }>();
  const [selected, setSelected] = useState<string[]>([]);
  const load = async (signal?: AbortSignal) => {
    const [scope, data] = await Promise.all([libraryRequest<LibraryTarget>("/api/v1/library/target", undefined, "GET", signal), libraryRequest<{ collections: Collection[] }>("/api/v1/library/collections", undefined, "GET", signal)]);
    setTarget(scope); setMode(scope.mode); setGroupID(scope.group_id || ""); setCollectionID(scope.collection_id || ""); setCollections(data.collections);
  };
  useEffect(() => { const controller = new AbortController(); void load(controller.signal).catch(error => { if (error.name !== "AbortError") setMessage(error.message); }); return () => controller.abort(); }, []);
  const collection = collections.find(item => item.id === collectionID);
  useEffect(() => { if (collection) { setSeries(collection.series_name_format); setMovies(collection.movies_name_format); } setPreview(undefined); }, [collection]);
  const run = async (action: () => Promise<void>) => { setBusy(true); setMessage(""); try { await action(); } catch (error) { setMessage((error as Error).message); } finally { setBusy(false); } };
  const create = () => run(async () => {
    const result = await libraryRequest<{ id: string }>("/api/v1/library/collections", { name, telegram_filter_id: Number(folderID) || 0 }, "POST");
    const data = await libraryRequest<{ collections: Collection[] }>("/api/v1/library/collections"); setCollections(data.collections); setCollectionID(result.id); setMessage("Carpeta guardada. Guarda el destino para utilizarla.");
  });
  return <section className="settings-card settings-wide collection-settings">
    <span className="settings-icon"><FolderOpen size={19} /></span><div className="settings-copy"><h3>Biblioteca principal</h3><p>Un grupo o una carpeta con tus propios cajones. Las ubicaciones de los STRM se conservan por separado.</p></div>
    <div className="collection-fields"><label><span>Organización</span><PillSelect label="Organización de biblioteca" value={mode} onChange={setMode} options={[{ value: "group", label: "Grupo principal" }, { value: "collection", label: "Carpeta principal" }]} /></label>
    {mode === "group" ? <label><span>Grupo del catálogo</span><PillSelect value={groupID} onChange={setGroupID} label="Selecciona un grupo" options={groups.filter(group => group.forum).map(group => ({ value: group.id, label: group.title }))} /></label> : <>
      <label><span>Carpeta de biblioteca</span><PillSelect value={collectionID} onChange={setCollectionID} label="Selecciona una carpeta" options={collections.map(item => ({ value: item.id, label: item.name }))} /></label>
      <details className="collection-create"><summary><Plus size={14} /> Añadir carpeta</summary><label><span>Nombre</span><input value={name} maxLength={12} onChange={event => setName(event.target.value)} /></label>
        <button type="button" className="test-connection" disabled={busy} onClick={() => void run(async () => { const data = await libraryRequest<{ folders: { id: number; title: string; shared: boolean }[] }>("/api/v1/library/folders"); setFolders(data.folders); })}><RefreshCw size={14} /> Ver carpetas Telegram</button>
        {folders.length > 0 && <PillSelect value={folderID} onChange={setFolderID} label="Carpeta Telegram existente" options={[{ value: "", label: "Crear una carpeta nueva" }, ...folders.filter(folder => !folder.shared).map(folder => ({ value: String(folder.id), label: folder.title }))]} />}
        <small>Una carpeta nueva se crea en Telegram al añadir el primer cajón válido.</small><button type="button" className="test-connection" disabled={busy || !name.trim()} onClick={() => void create()}><Plus size={14} /> Añadir</button>
      </details>
    </>}
    <button type="button" className="test-connection" disabled={busy || !target || (mode === "group" ? !groupID : !collectionID)} onClick={() => void run(async () => {
      const saved = await libraryRequest<LibraryTarget>("/api/v1/library/target", { mode, revision: target?.revision, ...(mode === "group" ? { group_id: groupID } : { collection_id: collectionID }) }, "PATCH"); setTarget(saved); window.dispatchEvent(new Event("tgstrm:library-update")); setMessage("Destino de biblioteca guardado.");
    })}>{busy ? <LoaderCircle className="spin" size={14} /> : <Save size={14} />} Guardar destino</button>
    {mode === "collection" && collection && <div className="collection-naming">
      <label><span>Formato de cajones de series</span><input value={series} onChange={event => setSeries(event.target.value)} maxLength={128} /></label><label><span>Formato de cajones de películas</span><input value={movies} onChange={event => setMovies(event.target.value)} maxLength={128} /></label>
      <small>Variables: {"{letra}"} y {"{parte}"}. Guardar solo afecta a los cajones nuevos.</small>
      <button type="button" className="test-connection" disabled={busy} onClick={() => void run(async () => { await libraryRequest(`/api/v1/library/collections/${collection.id}/naming`, { series_name_format: series, movies_name_format: movies, revision: collection.revision }, "PATCH"); const data = await libraryRequest<{ collections: Collection[] }>("/api/v1/library/collections"); setCollections(data.collections); setMessage("Formatos guardados sin renombrar los cajones existentes."); })}><Save size={14} /> Guardar formatos</button>
      <button type="button" className="test-connection" disabled={busy || collection.group_count === 0} onClick={() => void run(async () => { const data = await libraryRequest<{ revision: number; items: RenameItem[] }>(`/api/v1/library/collections/${collection.id}/naming/preview`, {}, "POST"); setPreview(data); setSelected(data.items.filter(item => item.available && !item.manual && !item.conflict && item.previous_title !== item.desired_title).map(item => item.id)); })}><RefreshCw size={14} /> Aplicar a cajones existentes</button>
      {preview && <div className="rename-preview"><strong>Revisa los nombres antes de aplicar</strong>{preview.items.map(item => <label key={item.id} className="rename-choice"><input type="checkbox" disabled={!item.available || item.conflict || item.previous_title === item.desired_title} checked={selected.includes(item.id)} onChange={event => setSelected(current => event.target.checked ? [...current, item.id] : current.filter(id => id !== item.id))} /><span>{item.previous_title} → <strong>{item.desired_title}</strong><small>{item.conflict ? "Nombre modificado en Telegram: requiere revisión" : item.manual ? "Nombre manual: excluido por defecto" : item.previous_title === item.desired_title ? "Ya coincide" : "Solo se renombra el cajón"}</small></span></label>)}<button type="button" className="test-connection" disabled={busy || selected.length === 0} onClick={() => void run(async () => { await libraryRequest(`/api/v1/library/collections/${collection.id}/naming/apply`, { revision: preview.revision, request_key: libraryRequestKey(), group_ids: selected, include_manual: true }, "POST"); setPreview(undefined); setMessage("Renombrado en segundo plano. Se comprobará cada cajón antes de cambiarlo."); })}><Check size={14} /> Confirmar {selected.length} nombres</button></div>}
    </div>}
    <LibraryCleanup />
    {message && <p className="collection-message" role="status">{message}</p>}</div>
  </section>;
}
