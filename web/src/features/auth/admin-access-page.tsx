import { useState } from "react";
import type { FormEvent } from "react";
import { KeyRound, ShieldCheck, Terminal, UserRound } from "lucide-react";
import { Link, useNavigate } from "react-router";
import { Brand } from "../../components/ui/brand";

async function send(path: string, body: Record<string, unknown>) {
  const response = await fetch(`/api/v1/admin${path}`, { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.message || "No se pudo completar la operación.");
  return data;
}

export function AdminAccessPage({ mode = "login" }: { mode?: "login" | "initial" | "recovery" }) {
  const navigate = useNavigate();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [recoveryStarted, setRecoveryStarted] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setError("");
    const values = new FormData(event.currentTarget);
    try {
      if (mode === "login") {
        const data = await send("/login", { username: values.get("username"), password: values.get("password") });
        navigate(data.must_change_password ? "/admin/change-password" : "/home", { replace: true });
      } else if (mode === "initial") {
        const password = String(values.get("password") || "");
        if (password !== values.get("confirmation")) throw new Error("Las contraseñas no coinciden.");
        await send("/password/initial", { password });
        navigate("/admin/login", { replace: true });
      } else {
        const password = String(values.get("password") || "");
        if (password !== values.get("confirmation")) throw new Error("Las contraseñas no coinciden.");
        await send("/recovery/complete", { code: values.get("code"), username: values.get("username"), password });
        navigate("/admin/login", { replace: true });
      }
    } catch (reason) { setError(reason instanceof Error ? reason.message : "No se pudo completar la operación."); }
    finally { setBusy(false); }
  }

  async function requestRecovery() {
    setBusy(true); setError("");
    try { const data = await send("/recovery/request", {}); setNotice(data.message); setRecoveryStarted(true); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "No se pudo solicitar el código."); }
    finally { setBusy(false); }
  }

  async function skipInitialPassword() {
    setBusy(true); setError("");
    try { await send("/password/skip", {}); navigate("/home", { replace: true }); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "No se pudo abrir el panel."); setBusy(false); }
  }

  const title = mode === "login" ? "Accede al panel." : mode === "initial" ? "Protege la cuenta inicial." : "Recupera el acceso.";
  const description = mode === "login" ? "Utiliza la cuenta administradora de TgStrm." : mode === "initial" ? "Te recomendamos sustituir la contraseña inicial. También puedes hacerlo más adelante desde Configuración." : "El código temporal se muestra únicamente en la consola donde se ejecuta el backend.";
  return <div className="access-page admin-access-page">
    <header className="site-header"><Brand /><span className="header-pill"><ShieldCheck size={14}/> Administración</span></header>
    <main className="access-main"><section className="auth-card" aria-labelledby="admin-access-title">
      <div className="admin-access-icon">{mode === "recovery" ? <Terminal/> : <KeyRound/>}</div>
      <p className="eyebrow">ACCESO SEGURO</p><h1 id="admin-access-title">{title}</h1><p className="description">{description}</p>
      {mode === "recovery" && !recoveryStarted ? <button className="submit-button" type="button" disabled={busy} onClick={() => void requestRecovery()}>{busy ? "Generando…" : "Mostrar código en la consola"}<Terminal size={17}/></button> :
      <form onSubmit={submit}>
        {mode !== "initial" && <label><span>Usuario</span><span className="input-shell"><UserRound size={17}/><input name="username" autoComplete="username" required minLength={3} maxLength={32} autoFocus /></span></label>}
        {mode === "recovery" && <label><span>Código temporal</span><span className="input-shell"><KeyRound size={17}/><input name="code" inputMode="text" autoComplete="one-time-code" required /></span></label>}
        <label><span>{mode === "login" ? "Contraseña" : "Nueva contraseña"}</span><span className="input-shell"><KeyRound size={17}/><input name="password" type="password" autoComplete={mode === "login" ? "current-password" : "new-password"} required minLength={mode === "login" ? 1 : 8} maxLength={128}/></span></label>
        {mode !== "login" && <label><span>Repite la contraseña</span><span className="input-shell"><KeyRound size={17}/><input name="confirmation" type="password" autoComplete="new-password" required minLength={8} maxLength={128}/></span></label>}
        {notice && <p className="form-notice" role="status">{notice}</p>}{error && <p className="form-error" role="alert">{error}</p>}
        <button className="submit-button" disabled={busy}>{busy ? "Comprobando…" : mode === "login" ? "Entrar" : mode === "initial" ? "Guardar contraseña" : "Restablecer acceso"}</button>
        {mode === "initial" && <button className="skip-button" type="button" disabled={busy} onClick={() => void skipInitialPassword()}>Ahora no</button>}
      </form>}
      <div className="admin-access-links">{mode === "login" ? <Link to="/admin/recovery">¿No recuerdas el acceso?</Link> : <Link to="/admin/login">Volver al inicio de sesión</Link>}</div>
    </section></main>
  </div>;
}
