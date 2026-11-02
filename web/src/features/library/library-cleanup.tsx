import { useEffect, useState } from "react";
import { LoaderCircle, RefreshCw, Trash2 } from "lucide-react";
import { libraryRequest } from "./api";
import type { LibraryTarget } from "./api";

type Preview = { target_name: string; scope: LibraryTarget; trees: { root: string; name: string }[]; exports: number; fingerprint: string; paused: boolean; active_job_id?: string };
type Job = { status: string; completed: number; total: number; last_error: string };
export function LibraryCleanup() {
  const [preview, setPreview] = useState<Preview>();
  const [confirmation, setConfirmation] = useState("");
  const [busy, setBusy] = useState(false);
  const [jobID, setJobID] = useState("");
  const [job, setJob] = useState<Job>();
  const [message, setMessage] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    void libraryRequest<Preview>("/api/v1/library/cleanup", undefined, "GET", controller.signal).then(value => { if (value.active_job_id) setJobID(value.active_job_id); }).catch(error => { if (!controller.signal.aborted) setMessage((error as Error).message); });
    return () => controller.abort();
  }, []);
  useEffect(() => {
    if (!jobID) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const poll = async () => {
      try {
        const value = await libraryRequest<Job>(`/api/v1/library/cleanup/${jobID}`, undefined, "GET", controller.signal);
        if (controller.signal.aborted) return;
        setJob(value);
        if (value.status === "completed" || value.status === "attention") {
          setJobID(""); setBusy(false); setPreview(undefined); setConfirmation("");
          setMessage(value.status === "completed" ? "Biblioteca generada limpia. El catálogo y TMDB se conservan; la generación del destino anterior queda pausada." : value.last_error);
          window.dispatchEvent(new Event("tgstrm:library-update"));
        } else timer = setTimeout(() => void poll(), 1500);
      } catch (error) {
        if (!controller.signal.aborted) { setMessage((error as Error).message); timer = setTimeout(() => void poll(), 5000); }
      }
    };
    void poll();
    return () => { controller.abort(); if (timer) clearTimeout(timer); };
  }, [jobID]);
  const prepare = async () => {
    setBusy(true); setMessage(""); setJob(undefined);
    try { const value = await libraryRequest<Preview>("/api/v1/library/cleanup"); setPreview(value); setConfirmation(""); if (value.active_job_id) setJobID(value.active_job_id); }
    catch (error) { setMessage((error as Error).message); }
    finally { setBusy(false); }
  };
  const execute = async (action: "clean" | "resume") => {
    if (!preview) return;
    setBusy(true); setMessage("");
    try {
      const value = await libraryRequest<{ id: string }>("/api/v1/library/cleanup", { fingerprint: preview.fingerprint, confirmation, action }, "POST");
      if (action === "clean") { setJobID(value.id); setPreview(undefined); }
      else { setPreview(undefined); setMessage("Generación automática reactivada para este destino."); setBusy(false); }
    } catch (error) { setMessage((error as Error).message); setBusy(false); }
  };
  const working = busy || !!jobID;
  return <div className="library-cleanup">
    <h4>Limpiar biblioteca generada</h4>
    <p>Prepara el cambio de organización. Conserva Telegram, las coincidencias TMDB, los enlaces estables y tus ajustes.</p>
    <button type="button" className="test-connection" disabled={working} onClick={() => void prepare()}>{working ? <LoaderCircle size={14} className="spin" /> : <Trash2 size={14} />} Revisar limpieza</button>
    {jobID && <p role="status">Limpiando {job?.completed || 0} de {job?.total || "…"} carpetas. Puedes salir de esta página.</p>}
    {preview && !jobID && <div className="library-cleanup-preview">
      <strong>{preview.target_name} · {preview.exports} STRM · {preview.trees.length} carpetas del destino guardado</strong>
      {preview.trees.length > 0 && <details><summary>Ver ubicaciones que se borrarán</summary><ul>{preview.trees.map(tree => <li key={`${tree.root}/${tree.name}`}>{tree.root}/{tree.name}</li>)}</ul></details>}
      <p>Se borrarán estas carpetas completas, incluidos sus pósters y metadatos, y los registros de generación. Las rutas principales se conservan. La generación automática de este destino quedará pausada.</p>
      <label><span>Escribe LIMPIAR para confirmar</span><input autoComplete="off" value={confirmation} onChange={event => setConfirmation(event.target.value)} /></label>
      <div className="library-cleanup-actions"><button type="button" className="test-connection library-cleanup-danger" disabled={working || confirmation !== "LIMPIAR"} onClick={() => void execute("clean")}><Trash2 size={14} /> Confirmar limpieza</button><button type="button" className="test-connection" disabled={working} onClick={() => setPreview(undefined)}>Cancelar</button>
      {preview.paused && <button type="button" className="test-connection" disabled={working} onClick={() => void execute("resume")}><RefreshCw size={14} /> Reactivar generación</button>}</div>
    </div>}
    {message && <p role="status">{message}</p>}
  </div>;
}
