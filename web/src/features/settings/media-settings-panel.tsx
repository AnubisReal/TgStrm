import { CollectionSettings } from "../library/collection-settings";
import { Activity, Bell, Check, ChevronDown, CircleCheck, Database, FolderCheck, Globe2, HardDrive, KeyRound, LoaderCircle, Radio, RefreshCw, Save, ShieldCheck, Trash2, TriangleAlert } from "lucide-react";
import { useEffect, useState } from "react";
import type { FormEvent } from "react";

type MediaSettings = {
  ambient_enabled: boolean; ambient_idle_minutes: number; ambient_window: "day" | "week";
  include_tmdb_id: boolean;
  public_base_url: string;
  movies_path: string;
  shows_path: string;
  series_naming: "universal" | "jellyfin_tmdb" | "compact";
  tmdb_configured: boolean;
  tmdb_verified: boolean;
  cache_limit_mb: number;
  cache_used_bytes: number;
  cache_entries: number;
  notification_level: "strict" | "balanced" | "essential";
};
type TelegramGroup = { id: string; title: string; forum: boolean; active: boolean };
type HealthComponent = { status: "ok" | "warning" | "error"; detail: string };
type SystemHealth = {
  checked_at: string; database: HealthComponent; telegram: HealthComponent; realtime: HealthComponent; mirror: HealthComponent; storage: HealthComponent;
  last_event_at?: string; last_sync_at?: string; mirror_active: number; mirror_pending: number; mirror_failed: number; pending_events: number;
};
type ReconciliationReport = { checked_at: string; exports_checked: number; missing_files: number; errored_exports: number; pending_exports: number; videos_without_asset: number; pending_events: number; mirror_pending: number; mirror_failed: number };
type BackupItem = { name: string; created_at: string; size: number; version: number; valid: boolean; restorable: boolean };
type BackupSettings = { enabled: boolean; frequency: "daily" | "weekly" | "interval"; hour: string; weekday: number; interval_hours: number; retention: number; last_run_at?: string; last_status?: string; last_error?: string; next_run_at?: string };
const defaultBackupSettings: BackupSettings = { enabled: false, frequency: "daily", hour: "03:00", weekday: 1, interval_hours: 24, retention: 7 };

const emptySettings: MediaSettings = {
  ambient_enabled: false, ambient_idle_minutes: 5, ambient_window: "week", include_tmdb_id: false,
  public_base_url: "",
  movies_path: "",
  shows_path: "",
  series_naming: "universal",
  tmdb_configured: false,
  tmdb_verified: false,
  cache_limit_mb: 512,
  cache_used_bytes: 0,
  cache_entries: 0,
  notification_level: "balanced",
};

const formatCacheBytes = (bytes: number) => {
  if (bytes <= 0) return "0 MB";
  return `${(bytes / (1024 * 1024)).toLocaleString("es", { maximumFractionDigits: 1 })} MB`;
};

export function MediaSettingsPanel() {
  const [settings, setSettings] = useState(emptySettings);
  const [token, setToken] = useState("");
  const [ambientPreviewError, setAmbientPreviewError] = useState("");
  useEffect(() => {
    const failed = (event: Event) => setAmbientPreviewError((event as CustomEvent<string>).detail);
    window.addEventListener("tgstrm:ambient-preview-failed", failed);
    return () => window.removeEventListener("tgstrm:ambient-preview-failed", failed);
  }, []);
  const [state, setState] = useState<"loading" | "ready" | "saving" | "saved" | "error">("loading");
  const [message, setMessage] = useState("");
  const [testingTMDB, setTestingTMDB] = useState(false);
  const [clearingCache, setClearingCache] = useState(false);
  const [groups, setGroups] = useState<TelegramGroup[]>([]);
  const [health, setHealth] = useState<SystemHealth | null>(null);
  const [checkingHealth, setCheckingHealth] = useState(false);
  const [healthCheckStep, setHealthCheckStep] = useState(-1);
  const [reconciliation, setReconciliation] = useState<ReconciliationReport | null>(null);
  const [checkingReconciliation, setCheckingReconciliation] = useState(false);
  const [repairingExports, setRepairingExports] = useState(false);
  const [backups, setBackups] = useState<BackupItem[]>([]);
  const [backupDirectory, setBackupDirectory] = useState("");
  const [creatingBackup, setCreatingBackup] = useState(false);
  const [backupSettings, setBackupSettings] = useState(defaultBackupSettings);
  const [savingBackupSettings, setSavingBackupSettings] = useState(false);
  const [restoreTarget, setRestoreTarget] = useState<BackupItem | null>(null);
  const [restoreConfirmation, setRestoreConfirmation] = useState("");
  const [restoring, setRestoring] = useState(false);
  const [restoreResult, setRestoreResult] = useState<{ safety_backup: string } | null>(null);

  const loadHealth = async () => {
    setCheckingHealth(true);
    setHealthCheckStep(0);
    try {
      const request = fetch("/api/v1/health/system", { credentials: "same-origin" });
      for (let step = 0; step < 5; step += 1) {
        setHealthCheckStep(step);
        await new Promise((resolve) => window.setTimeout(resolve, 260));
      }
      const response = await request;
      if (!response.ok) throw new Error("No se pudo comprobar la salud del sistema.");
      setHealth(await response.json() as SystemHealth);
    } finally { setCheckingHealth(false); setHealthCheckStep(-1); }
  };

  const checkReconciliation = async () => {
    setCheckingReconciliation(true);
    setMessage("");
    try {
      const response = await fetch("/api/v1/health/reconciliation", { credentials: "same-origin" });
      const data = await response.json() as ReconciliationReport & { message?: string };
      if (!response.ok) throw new Error(data.message || "No se pudo revisar la coherencia.");
      setReconciliation(data);
    } catch (error) {
      setMessage((error as Error).message);
      setState("error");
    } finally { setCheckingReconciliation(false); }
  };

  const repairMissingExports = async () => {
    if (!reconciliation || reconciliation.missing_files === 0) return;
    setRepairingExports(true);
    setMessage("");
    try {
      const response = await fetch("/api/v1/health/reconciliation/repair", { method: "POST", credentials: "same-origin" });
      const data = await response.json() as { written?: number; failed?: number; message?: string };
      if (!response.ok) throw new Error(data.message || "No se pudieron reparar los STRM.");
      setMessage(data.failed ? `${data.written || 0} STRM reconstruidos; ${data.failed} requieren revisión.` : `${data.written || 0} STRM reconstruidos correctamente.`);
      await checkReconciliation();
    } catch (error) {
      setMessage((error as Error).message);
      setState("error");
    } finally { setRepairingExports(false); }
  };

  useEffect(() => {
    const controller = new AbortController();
    Promise.all([
      fetch("/api/v1/settings/media", { credentials: "same-origin", signal: controller.signal }),
      fetch("/api/v1/groups", { credentials: "same-origin", signal: controller.signal }),
      fetch("/api/v1/health/system", { credentials: "same-origin", signal: controller.signal }),
      fetch("/api/v1/backups", { credentials: "same-origin", signal: controller.signal }),
      fetch("/api/v1/settings/backups", { credentials: "same-origin", signal: controller.signal }),
    ])
      .then(async ([settingsResponse, groupsResponse, healthResponse, backupsResponse, backupSettingsResponse]) => {
        if (!settingsResponse.ok || !groupsResponse.ok || !healthResponse.ok || !backupsResponse.ok || !backupSettingsResponse.ok) throw new Error("No se pudo leer la configuración.");
        return Promise.all([settingsResponse.json() as Promise<MediaSettings>, groupsResponse.json() as Promise<{ groups: TelegramGroup[] }>, healthResponse.json() as Promise<SystemHealth>, backupsResponse.json() as Promise<{ backups: BackupItem[]; directory: string }>, backupSettingsResponse.json() as Promise<BackupSettings>]);
      })
      .then(([data, groupData, healthData, backupData, backupPolicy]) => {
        setSettings(data);
      setGroups(groupData.groups); setHealth(healthData); setBackups(backupData.backups); setBackupDirectory(backupData.directory); setBackupSettings(backupPolicy); setState("ready");
      })
      .catch((error: unknown) => {
        if ((error as { name?: string }).name !== "AbortError") {
          setMessage((error as Error).message);
          setState("error");
        }
      });
    return () => controller.abort();
  }, []);

  const save = async (event: FormEvent) => {
    event.preventDefault();
    setState("saving");
    setMessage("");
    try {
      const response = await fetch("/api/v1/settings/media", {
        method: "PUT",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ...settings, tmdb_token: token }),
      });
      const data = await response.json() as MediaSettings & { message?: string };
      if (!response.ok) throw new Error(data.message || "No se pudo guardar la configuración.");
      setSettings(data);
      window.dispatchEvent(new Event("tgstrm:media-settings-saved"));
      setToken("");
      setState("saved");
      window.setTimeout(() => setState("ready"), 1800);
    } catch (error) {
      setMessage((error as Error).message);
      setState("error");
    }
  };

  const testTMDB = async () => {
    setTestingTMDB(true);
    setMessage("");
    try {
      const response = await fetch("/api/v1/settings/media/test-tmdb", { method: "POST", credentials: "same-origin" });
      const data = await response.json() as { message?: string; tmdb_verified?: boolean };
      if (!response.ok) throw new Error(data.message || "No se pudo comprobar TMDB.");
      setSettings((current) => ({ ...current, tmdb_verified: true }));
    } catch (error) {
      setSettings((current) => ({ ...current, tmdb_verified: false }));
      setMessage((error as Error).message);
      setState("error");
    } finally { setTestingTMDB(false); }
  };

  const clearCache = async () => {
    setClearingCache(true);
    setMessage("");
    try {
      const response = await fetch("/api/v1/settings/media/cache", { method: "DELETE", credentials: "same-origin" });
      const data = await response.json() as { used_bytes?: number; entries?: number; message?: string };
      if (!response.ok) throw new Error(data.message || "No se pudo vaciar la caché.");
      setSettings((current) => ({ ...current, cache_used_bytes: data.used_bytes || 0, cache_entries: data.entries || 0 }));
    } catch (error) {
      setMessage((error as Error).message);
      setState("error");
    } finally { setClearingCache(false); }
  };

  const createBackup = async () => {
    setCreatingBackup(true);
    setMessage("");
    try {
      const response = await fetch("/api/v1/backups", { method: "POST", credentials: "same-origin" });
      const data = await response.json() as BackupItem & { message?: string };
      if (!response.ok) throw new Error(data.message || "No se pudo crear la copia de seguridad.");
      setBackups((current) => [data, ...current.filter((item) => item.name !== data.name)]);
    } catch (error) {
      setMessage((error as Error).message);
      setState("error");
    } finally { setCreatingBackup(false); }
  };

  const saveBackupPolicy = async () => {
    setSavingBackupSettings(true);
    setMessage("");
    try {
      const response = await fetch("/api/v1/settings/backups", { method: "PUT", credentials: "same-origin", headers: { "Content-Type": "application/json" }, body: JSON.stringify(backupSettings) });
      const data = await response.json() as BackupSettings & { message?: string };
      if (!response.ok) throw new Error(data.message || "No se pudo guardar la programación.");
      setBackupSettings(data);
    } catch (error) {
      setMessage((error as Error).message);
      setState("error");
    } finally { setSavingBackupSettings(false); }
  };

  const restoreBackup = async () => {
    if (!restoreTarget || restoreConfirmation !== "RESTAURAR") return;
    setRestoring(true);
    setMessage("");
    try {
      const response = await fetch(`/api/v1/backups/${encodeURIComponent(restoreTarget.name)}/restore`, { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ confirmation: restoreConfirmation }) });
      const data = await response.json() as { message?: string; safety_backup?: string };
      if (!response.ok) throw new Error(data.message || "No se pudo restaurar la copia.");
      setRestoreResult({ safety_backup: data.safety_backup || "" });
    } catch (error) {
      setMessage((error as Error).message);
      setState("error");
      setRestoreTarget(null);
      setRestoreConfirmation("");
    } finally { setRestoring(false); }
  };

  if (state === "loading") return <section className="settings-loading"><LoaderCircle size={22} /> Cargando ajustes…</section>;

  return (
    <form className="settings-panel" onSubmit={save}>
      <header className="settings-heading">
        <div><p className="eyebrow">BIBLIOTECA</p><h2>Medios y STRM</h2></div>
        <button className="settings-save" type="submit" disabled={state === "saving"}>
          {state === "saving" ? <LoaderCircle className="spin" size={16} /> : state === "saved" ? <Check size={16} /> : <Save size={16} />}
          {state === "saving" ? "Guardando" : state === "saved" ? "Guardado" : "Guardar"}
        </button>
      </header>

      <div className="settings-grid">
        <section className="settings-card settings-wide health-card">
          <span className="settings-icon"><Activity size={19} /></span>
          <div className="settings-copy"><h3>Salud del sistema</h3><p>Estado real de los servicios que mantienen el catálogo sincronizado.</p></div>
          <div className="health-summary">
            <div className="health-summary-head"><span>{checkingHealth ? `Comprobando ${["PostgreSQL", "Telegram", "tiempo real", "Espejo", "almacenamiento"][healthCheckStep] || "servicios"}…` : health?.checked_at ? `Comprobado ${new Date(health.checked_at).toLocaleTimeString("es", { hour: "2-digit", minute: "2-digit" })}` : "Comprobación pendiente"}</span><button type="button" className="test-connection" onClick={loadHealth} disabled={checkingHealth}><RefreshCw className={checkingHealth ? "spin" : ""} size={14} /> {checkingHealth ? `${healthCheckStep + 1}/5` : "Comprobar"}</button></div>
            <div className="health-items">
              {health && ([
                ["PostgreSQL", health.database, <Database size={15} />], ["Telegram", health.telegram, <Radio size={15} />],
                ["Tiempo real", health.realtime, <Activity size={15} />], ["Espejo", health.mirror, <RefreshCw size={15} />], ["Almacenamiento", health.storage, <FolderCheck size={15} />],
              ] as const).map(([label, component, icon], index) => <div className={`health-item ${component.status}${checkingHealth && index === healthCheckStep ? " checking" : ""}${checkingHealth && index > healthCheckStep ? " waiting" : ""}`} key={label} title={component.detail}><span className="health-item-icon">{icon}</span><span><strong>{label}</strong><small>{checkingHealth && index === healthCheckStep ? "Comprobando ahora…" : component.detail}</small></span>{checkingHealth && index === healthCheckStep ? <LoaderCircle className="spin" size={16} /> : component.status === "ok" ? <CircleCheck size={16} /> : <TriangleAlert size={16} />}</div>)}
            </div>
            {health && <div className="health-meta"><span>{health.mirror_active} espejos activos</span><span>{health.mirror_pending} pendientes</span><span>{health.pending_events} eventos en cola</span><span>Último evento: {health.last_event_at ? new Date(health.last_event_at).toLocaleString("es", { dateStyle: "short", timeStyle: "short" }) : "sin datos"}</span></div>}
            <div className="reconciliation-summary"><div><strong>Coherencia del catálogo</strong><small>Revisa Telegram, PostgreSQL y los STRM sin cambiar datos.</small></div><button type="button" className="test-connection" onClick={checkReconciliation} disabled={checkingReconciliation}>{checkingReconciliation ? <LoaderCircle className="spin" size={14} /> : <RefreshCw size={14} />}{checkingReconciliation ? "Revisando…" : "Revisar"}</button></div>
            {reconciliation && <><div className="reconciliation-items"><span>{reconciliation.exports_checked} STRM comprobados</span><span className={reconciliation.missing_files + reconciliation.errored_exports > 0 ? "warning" : "ok"}>{reconciliation.missing_files + reconciliation.errored_exports > 0 ? `${reconciliation.missing_files + reconciliation.errored_exports} incidencias STRM` : "STRM coherentes"}</span><span className={reconciliation.pending_exports + reconciliation.videos_without_asset > 0 ? "warning" : "ok"}>{reconciliation.pending_exports + reconciliation.videos_without_asset > 0 ? `${reconciliation.pending_exports + reconciliation.videos_without_asset} elementos pendientes` : "Sin trabajo pendiente"}</span><span>{reconciliation.mirror_pending + reconciliation.mirror_failed} entregas Espejo pendientes</span></div>{reconciliation.missing_files > 0 && <button type="button" className="repair-exports" onClick={repairMissingExports} disabled={repairingExports}>{repairingExports ? <LoaderCircle className="spin" size={14} /> : <RefreshCw size={14} />}{repairingExports ? "Reconstruyendo…" : `Reconstruir ${reconciliation.missing_files} STRM ausentes`}</button>}</>}
          </div>
        </section>
        <section className="settings-card settings-wide backup-card">
          <span className="settings-icon"><ShieldCheck size={19} /></span>
          <div className="settings-copy"><h3>Copias de seguridad</h3><p>Protege PostgreSQL y la configuración cifrada. Las bibliotecas STRM y la caché permanecen fuera de la copia.</p></div>
          <div className="backup-panel">
            <div className="backup-actions"><span title={backupDirectory}>{backupDirectory || "data/backups"}</span><button type="button" className="test-connection" onClick={createBackup} disabled={creatingBackup}>{creatingBackup ? <LoaderCircle className="spin" size={14} /> : <ShieldCheck size={14} />}{creatingBackup ? "Creando copia…" : "Crear copia ahora"}</button></div>
            <div className="backup-policy">
              <label className="backup-toggle"><input type="checkbox" checked={backupSettings.enabled} onChange={(event) => setBackupSettings({ ...backupSettings, enabled: event.target.checked })} /><span><i />Automáticas</span></label>
              <span className="settings-select-control"><select value={backupSettings.frequency} onChange={(event) => setBackupSettings({ ...backupSettings, frequency: event.target.value as BackupSettings["frequency"] })}><option value="daily">Cada día</option><option value="weekly">Cada semana</option><option value="interval">Por intervalo</option></select><i aria-hidden="true"><ChevronDown size={14} /></i></span>
              {backupSettings.frequency === "interval" ? <label className="backup-number"><span>Cada</span><input type="number" min="1" max="168" value={backupSettings.interval_hours} onChange={(event) => setBackupSettings({ ...backupSettings, interval_hours: Number(event.target.value) })} /><span>h</span></label> : <input aria-label="Hora de la copia" type="time" value={backupSettings.hour} onChange={(event) => setBackupSettings({ ...backupSettings, hour: event.target.value })} />}
              {backupSettings.frequency === "weekly" && <span className="settings-select-control"><select value={backupSettings.weekday} onChange={(event) => setBackupSettings({ ...backupSettings, weekday: Number(event.target.value) })}>{["Domingo", "Lunes", "Martes", "Miércoles", "Jueves", "Viernes", "Sábado"].map((day, index) => <option value={index} key={day}>{day}</option>)}</select><i aria-hidden="true"><ChevronDown size={14} /></i></span>}
              <label className="backup-number"><span>Conservar</span><input type="number" min="1" max="100" value={backupSettings.retention} onChange={(event) => setBackupSettings({ ...backupSettings, retention: Number(event.target.value) })} /></label>
              <button type="button" className="test-connection" onClick={saveBackupPolicy} disabled={savingBackupSettings}>{savingBackupSettings ? <LoaderCircle className="spin" size={14} /> : <Save size={14} />} Guardar programación</button>
            </div>
            <div className="backup-schedule-state"><span>{backupSettings.enabled ? backupSettings.next_run_at ? `Próxima: ${new Date(backupSettings.next_run_at).toLocaleString("es", { dateStyle: "short", timeStyle: "short" })}` : "Calculando próxima copia…" : "Copias automáticas desactivadas"}</span>{backupSettings.last_run_at && <span className={backupSettings.last_status === "failed" ? "failed" : ""}>Última: {new Date(backupSettings.last_run_at).toLocaleString("es", { dateStyle: "short", timeStyle: "short" })}{backupSettings.last_error ? ` · ${backupSettings.last_error}` : ""}</span>}</div>
            {backups.length > 0 ? <div className="backup-list">{backups.slice(0, 4).map((backup) => <div className="backup-item" key={backup.name}><span className={`backup-validity${backup.valid ? " valid" : ""}`}>{backup.valid ? <CircleCheck size={15} /> : <TriangleAlert size={15} />}</span><span><strong>{new Date(backup.created_at).toLocaleString("es", { dateStyle: "medium", timeStyle: "short" })}</strong><small>{(backup.size / (1024 * 1024)).toLocaleString("es", { maximumFractionDigits: 1 })} MB · Formato v{backup.version || "?"}{backup.valid && !backup.restorable ? " · Copia anterior sin identidad verificable" : ""}</small></span>{backup.restorable && <button type="button" className="backup-restore" onClick={() => { setRestoreTarget(backup); setRestoreConfirmation(""); }}>Restaurar</button>}</div>)}</div> : <div className="backup-empty">Todavía no hay copias guardadas.</div>}
          </div>
        </section>
        {restoreTarget && !restoreResult && <section className="restore-confirmation" role="dialog" aria-modal="true" aria-labelledby="restore-title">
          <div><span className="restore-warning"><TriangleAlert size={19} /></span><div><h3 id="restore-title">Restaurar esta copia</h3><p>Se creará una copia preventiva, se detendrán Telegram y el Espejo y PostgreSQL se restaurará en una transacción. TgStrm deberá reiniciarse al terminar.</p></div></div>
          <strong>{new Date(restoreTarget.created_at).toLocaleString("es", { dateStyle: "long", timeStyle: "short" })}</strong>
          <label><span>Escribe RESTAURAR para continuar</span><input autoFocus value={restoreConfirmation} onChange={(event) => setRestoreConfirmation(event.target.value)} /></label>
          <div className="restore-actions"><button type="button" className="test-connection" onClick={() => { setRestoreTarget(null); setRestoreConfirmation(""); }} disabled={restoring}>Cancelar</button><button type="button" className="restore-submit" onClick={restoreBackup} disabled={restoring || restoreConfirmation !== "RESTAURAR"}>{restoring ? <LoaderCircle className="spin" size={15} /> : <RefreshCw size={15} />}{restoring ? "Restaurando…" : "Restaurar y detener"}</button></div>
        </section>}
        {restoreResult && <section className="restore-confirmation restored" role="status"><div><span className="restore-warning"><CircleCheck size={19} /></span><div><h3>Restauración completada</h3><p>La copia preventiva es {restoreResult.safety_backup}. Reinicia TgStrm para volver a conectar PostgreSQL, Telegram y los procesos en tiempo real.</p></div></div></section>}
        <CollectionSettings groups={groups} />

        <section className="settings-card settings-wide notification-settings-card">
          <span className="settings-icon"><Bell size={19} /></span>
          <div className="settings-copy"><h3>Notificaciones</h3><p>El historial de Actividad siempre se conserva. Este nivel decide qué aparece en la campana.</p></div>
          <label><span>Nivel de avisos</span><span className="settings-select-control"><select value={settings.notification_level} onChange={(event) => setSettings({ ...settings, notification_level: event.target.value as MediaSettings["notification_level"] })}>
            <option value="strict">Estricto · toda la actividad</option>
            <option value="balanced">Equilibrado · resultados e incidencias</option>
            <option value="essential">Esencial · solo requiere atención</option>
          </select><i aria-hidden="true"><ChevronDown size={15} /></i></span><small>{settings.notification_level === "strict" ? "Muestra procesos, resultados correctos y errores." : settings.notification_level === "balanced" ? "Muestra trabajos terminados, fallos y elementos pendientes de revisión." : "Muestra únicamente fallos y elementos que necesitan una decisión."}</small></label>
        </section>
        <section className="settings-card settings-wide">
          <span className="settings-icon"><Globe2 size={19} /></span>
          <div className="settings-copy"><h3>Dirección pública</h3><p>Es la dirección estable que se escribirá dentro de cada archivo STRM.</p></div>
          <label><span>URL pública del servidor</span><input type="url" placeholder="Se puede configurar más adelante" value={settings.public_base_url} onChange={(event) => setSettings({ ...settings, public_base_url: event.target.value })} /></label>
        </section>

        <section className="settings-card">
          <span className="settings-icon"><Database size={19} /></span>
          <div className="settings-copy"><h3>Destinos de biblioteca</h3><p>Películas y series se guardan por separado para que el servidor multimedia las identifique bien.</p></div>
          <label><span>Carpeta de películas</span><input placeholder="data/library/movies" value={settings.movies_path} onChange={(event) => setSettings({ ...settings, movies_path: event.target.value })} /></label>
          <label><span>Carpeta de series</span><input placeholder="data/library/shows" value={settings.shows_path} onChange={(event) => setSettings({ ...settings, shows_path: event.target.value })} /></label>
        </section>

        <section className="settings-card">
          <span className="settings-icon">
            <HardDrive size={19} />
          </span>
          <div className="settings-copy">
            <h3>Estructura de biblioteca</h3>
            <p>
              Define los nombres STRM y las temporadas. El perfil Jellyfin
              también permite varias versiones de una película en su carpeta. El
              identificador TMDB es opcional.
            </p>
          </div>
          <label>
            <span>Formato de biblioteca</span>
            <span className="settings-select-control">
              <select
                value={settings.series_naming}
                onChange={(event) =>
                  setSettings({
                    ...settings,
                    series_naming: event.target
                      .value as MediaSettings["series_naming"],
                  })
                }
              >
                <option value="universal">
                  Universal · Jellyfin, Plex y Kodi
                </option>
                <option value="jellyfin_tmdb">
                  Jellyfin · varias versiones
                </option>
                <option value="compact">Compacto · código y título</option>
              </select>
              <i aria-hidden="true">
                <ChevronDown size={15} />
              </i>
            </span>
            <small>
              {settings.series_naming === "universal"
                ? "Arrow (2012) / Season 01 / Arrow (2012) - S01E01 - Pilot.strm"
                : settings.series_naming === "jellyfin_tmdb"
                  ? `Arrow (2012)${settings.include_tmdb_id ? " [tmdbid-1412]" : ""} / Season 01 / Arrow S01E01 - Pilot.strm`
                  : "Arrow (2012) / Season 01 / S01E01 - Pilot.strm"}
            </small>
          </label>
          {settings.series_naming === "jellyfin_tmdb" && (
            <>
              <label className="backup-toggle">
                <input
                  type="checkbox"
                  checked={settings.include_tmdb_id ?? false}
                  onChange={(event) =>
                    setSettings({
                      ...settings,
                      include_tmdb_id: event.target.checked,
                    })
                  }
                />
                <span>
                  <i />
                  Incluir ID de TMDB en las carpetas
                </span>
              </label>
              <small>
                Desactivado: título y año. Activado: añade [tmdbid-ID] a
                películas y series. Los archivos existentes solo cambian al
                regenerar sus STRM.
              </small>
            </>
          )}
        </section>

        <section className="settings-card">
          <span className="settings-icon">
            <Globe2 size={19} />
          </span>
          <div className="settings-copy">
            <h3>Modo ambiente</h3>
            <p>
              Tendencias de TMDB cuando no estés usando la herramienta. No
              bloquea tu sesión.
            </p>
          </div>
          <button type="button" className="test-connection" disabled={!settings.tmdb_configured} onClick={()=>{setAmbientPreviewError("");window.dispatchEvent(new Event("tgstrm:ambient-preview"));}}>Probar salvapantallas</button>
          {ambientPreviewError && <small role="status">{ambientPreviewError}</small>}
<label className="backup-toggle ambient-toggle">
            <input
              type="checkbox"
              checked={settings.ambient_enabled ?? false}
              onChange={(event) =>
                setSettings({
                  ...settings,
                  ambient_enabled: event.target.checked,
                })
              }
            />
            <span>
              <i />
              Activar salvapantallas
            </span>
          </label>
          <label>
            <span>Espera de inactividad (minutos)</span>
            <input
              type="number"
              min="1"
              max="120"
              value={settings.ambient_idle_minutes || 5}
              onChange={(event) =>
                setSettings({
                  ...settings,
                  ambient_idle_minutes: Number(event.target.value),
                })
              }
            />
          </label>
          <label>
            <span>Tendencias</span>
            <span className="settings-select-control">
              <select
                value={settings.ambient_window || "week"}
                onChange={(event) =>
                  setSettings({
                    ...settings,
                    ambient_window: event.target.value as "day" | "week",
                  })
                }
              >
                <option value="day">Del día</option>
                <option value="week">De la semana</option>
              </select>
              <i aria-hidden="true">
                <ChevronDown size={15} />
              </i>
            </span>
          </label>
          <small>
            Requiere TMDB configurado. No se activa durante una reproducción, un
            formulario o un diálogo. La pantalla completa depende del navegador
            y se activa desde el salvapantallas.
          </small>
        </section>

        <section className="settings-card">
          <span className="settings-icon"><KeyRound size={19} /></span>
          <div className="settings-copy"><h3>TMDB</h3><p>Admite la API Key v3 y el Read Access Token oficial para identificar títulos y crear nombres consistentes.</p></div>
          <label><span>API Key o Read Access Token</span><input type="password" autoComplete="off" placeholder={settings.tmdb_configured ? "Configurado · escribe sólo para reemplazarlo" : "Introduce tu credencial de TMDB"} value={token} onChange={(event) => setToken(event.target.value)} /></label>
          <div className="secret-actions">
            <div className={`secret-state${settings.tmdb_verified ? " verified" : settings.tmdb_configured ? " configured" : ""}`}><i />{settings.tmdb_verified ? "Credencial verificada con TMDB" : settings.tmdb_configured ? "Credencial guardada y cifrada" : "Credencial pendiente"}</div>
            <button type="button" className="test-connection" onClick={testTMDB} disabled={!settings.tmdb_configured || testingTMDB}>{testingTMDB ? "Comprobando…" : "Probar conexión"}</button>
          </div>
          <small className="tmdb-attribution">This product uses the TMDB API but is not endorsed or certified by TMDB.</small>
        </section>

        <section className="settings-card">
          <span className="settings-icon"><HardDrive size={19} /></span>
          <div className="settings-copy"><h3>Caché multimedia</h3><p>Conserva el inicio y el índice de cada MP4 para acelerar aperturas posteriores. El contenido reproducido no se guarda completo.</p></div>
          <label><span>Límite máximo en MB</span><input type="number" min="64" max="8192" step="64" value={settings.cache_limit_mb} onChange={(event) => setSettings({ ...settings, cache_limit_mb: Number(event.target.value) })} /></label>
          <div className="cache-usage">
            <div><strong>{formatCacheBytes(settings.cache_used_bytes)}</strong><span>{settings.cache_entries.toLocaleString("es")} bloques guardados</span></div>
            <button type="button" className="test-connection cache-clear" onClick={clearCache} disabled={clearingCache || settings.cache_entries === 0}><Trash2 size={14} />{clearingCache ? "Vaciando…" : "Vaciar caché"}</button>
          </div>
        </section>
      </div>
      {state === "error" && <p className="settings-error" role="alert">{message}</p>}
    </form>
  );
}
