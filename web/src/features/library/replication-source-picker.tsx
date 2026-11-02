import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { CheckSquare2, LoaderCircle, Send, Film } from "lucide-react";
import { PillSelect } from "../../components/ui/pill-select";
import { libraryRequest } from "./api";
import type { Collection, LibraryTarget } from "./api";

export function ReplicationSourcePicker({ groups, selected, selecting, onSelected, onSelecting }: { groups: { id: string; forum: boolean; active: boolean; managed?: boolean }[]; selected: string[]; selecting: boolean; onSelected: (ids: string[]) => void; onSelecting: (value: boolean) => void }) {
  const navigate = useNavigate();
  const [collections, setCollections] = useState<Collection[]>([]);
  const [destination, setDestination] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    void Promise.all([libraryRequest<{ collections: Collection[] }>("/api/v1/library/collections", undefined, "GET", controller.signal), libraryRequest<LibraryTarget>("/api/v1/library/target", undefined, "GET", controller.signal)]).then(([data, target]) => { setCollections(data.collections); setDestination(target.collection_id || ""); }).catch(cause => { if (!controller.signal.aborted) setError(cause.message); });
    return () => controller.abort();
  }, []);
  const prepare = async () => {
    setBusy(true); setError("");
    try { const result = await libraryRequest<{ review_url: string }>("/api/v1/library/replication-plans", { collection_id: destination, group_ids: selected }, "POST"); navigate(result.review_url); }
    catch (cause) { setError((cause as Error).message); }
    finally { setBusy(false); }
  };
  if (!collections.length) return error ? <p role="alert">{error}</p> : null;
  return <div className="management-controls replication-source-management">
    <section className="selection-bar replication-source-controls" aria-label="Gestión de cajones">
      <div className="replication-source-selection">
        <button type="button" className="replication-source-action" onClick={()=>navigate("/mirror/manage?movies=1")}><Film size={15}/> Monitorizar películas</button>
        <button type="button" className="replication-source-action" aria-pressed={selecting} onClick={() => { onSelecting(!selecting); onSelected([]); }}><CheckSquare2 size={15} /> {selecting ? "Terminar selección" : "Seleccionar cajones"}</button>
        {selecting && <>
          <button type="button" className="replication-source-action" title="Seleccionar cajones visibles" onClick={() => onSelected(Array.from(new Set([...selected, ...groups.filter(group => group.forum && !group.active && !group.managed).map(group => group.id)])))}>Visibles</button>
          <button type="button" className="replication-source-action" onClick={() => onSelected([])}>Ninguno</button>
        </>}
      </div>
      {selecting && <div className="replication-source-destination">
        <PillSelect label="Carpeta destino" value={destination} onChange={setDestination} options={collections.map(collection => ({ value: collection.id, label: collection.name }))} />
        <button type="button" className="selection-primary replication-source-action" disabled={busy || !selected.length || selected.length > 200 || !destination} onClick={() => void prepare()}>{busy ? <LoaderCircle className="spin" size={15} /> : <Send size={15} />} Revisar {selected.length} cajones</button>
      </div>}
      {error && <p role="alert">{error}</p>}
    </section>
  </div>;
}
