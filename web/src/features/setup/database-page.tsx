import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { useNavigate } from "react-router";
import { ArrowRight, Database } from "lucide-react";
import { Brand } from "../../components/ui/brand";

type SetupState = "loading" | "unconfigured" | "ready" | "recovery" | "offline";
export function DatabasePage() {
  const navigate = useNavigate();
  const [state, setState] = useState<SetupState>("loading");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState(false);
  const [tls, setTLS] = useState("disable");
  useEffect(() => {
    const controller = new AbortController();
    fetch("/api/v1/setup/status", { signal: controller.signal })
      .then(async (r) => {
        if (!r.ok) throw new Error();
        const d = await r.json();
        if (!["unconfigured", "ready", "recovery"].includes(d.database))
          throw new Error();
        setState(d.database);
      })
      .catch((e) => {
        if (e.name !== "AbortError") setState("offline");
      });
    return () => controller.abort();
  }, []);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const values = new FormData(form);
    const action =
      (event.nativeEvent as SubmitEvent).submitter?.getAttribute("value") ??
      "test";
    setBusy(true);
    setMessage("");
    setError(false);
    try {
      const response = await fetch(
        `/api/v1/setup/database${action === "test" ? "/test" : ""}`,
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Setup-Token": String(values.get("token")),
          },
          body: JSON.stringify({
            host: values.get("host"),
            port: Number(values.get("port")),
            name: values.get("name"),
            user: values.get("user"),
            password: values.get("password"),
            ssl_mode: tls,
            root_cert: values.get("root_cert") || "",
          }),
        },
      );
      const data = await response.json();
      if (!response.ok)
        throw new Error(data.message || "No se pudo completar la operación.");
      if (action === "save") {
        form.reset();
        navigate("/login", { replace: true });
      } else {
        setMessage(data.message);
      }
    } catch (e) {
      setError(true);
      setMessage(
        e instanceof Error ? e.message : "No se pudo conectar con el servidor.",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="access-page">
      <header className="site-header">
        <Brand />
        <span className="header-pill">Almacenamiento</span>
      </header>
      <main className="access-main">
        <section
          className="auth-card database-card"
          aria-labelledby="database-title"
        >
          <Database className="section-icon" size={24} />
          <h1 id="database-title">Conecta tu base de datos.</h1>
          <p className="description">
            PostgreSQL guardará la configuración y el catálogo de tu servidor.
          </p>
          {state === "loading" && <p role="status">Comprobando instalación…</p>}
          {state === "offline" && (
            <p role="alert" className="notice">
              El servidor no está disponible. Inicia la API y recarga esta
              página.
            </p>
          )}
          {state === "ready" && (
            <div className="connection-state">
              <strong>Base de datos configurada</strong>
              <p>
                La conexión está guardada. La vinculación real con Telegram es
                el siguiente paso.
              </p>
            </div>
          )}
          {state === "recovery" && (
            <p role="alert" className="notice">
              Hay una configuración guardada, pero la base de datos no está
              disponible o su versión no es compatible. No se creará una
              instalación nueva. Revisa PostgreSQL y reinicia la API.
            </p>
          )}
          {state === "unconfigured" && (
            <form onSubmit={submit}>
              <fieldset disabled={busy}>
                <div className="field">
                  <label htmlFor="token">Código de instalación</label>
                  <input
                    id="token"
                    name="token"
                    type="password"
                    autoComplete="off"
                    required
                  />
                  <p className="field-help">
                    Aparece en la terminal al iniciar TgStrm por primera vez.
                  </p>
                </div>
                <div className="field-row">
                  <div className="field">
                    <label htmlFor="host">Servidor</label>
                    <input
                      id="host"
                      name="host"
                      defaultValue="127.0.0.1"
                      required
                      autoComplete="off"
                    />
                  </div>
                  <div className="field">
                    <label htmlFor="port">Puerto</label>
                    <input
                      id="port"
                      name="port"
                      type="number"
                      min="1"
                      max="65535"
                      defaultValue="5432"
                      required
                    />
                  </div>
                </div>
                <div className="field">
                  <label htmlFor="name">Base de datos</label>
                  <input
                    id="name"
                    name="name"
                    placeholder="tgstrm"
                    required
                    autoComplete="off"
                  />
                  <p className="field-help">
                    Debe existir. TgStrm creará su esquema dentro de ella.
                  </p>
                </div>
                <div className="field-row equal">
                  <div className="field">
                    <label htmlFor="user">Usuario</label>
                    <input id="user" name="user" required autoComplete="off" />
                  </div>
                  <div className="field">
                    <label htmlFor="db-password">Contraseña</label>
                    <input
                      id="db-password"
                      name="password"
                      type="password"
                      autoComplete="off"
                    />
                  </div>
                </div>
                <div className="field">
                  <label htmlFor="tls">Conexión TLS</label>
                  <select
                    id="tls"
                    value={tls}
                    onChange={(e) => setTLS(e.target.value)}
                  >
                    <option value="disable">Sin TLS · conexión local</option>
                    <option value="require">Exigir cifrado</option>
                    <option value="verify-full">
                      Verificar certificado y servidor
                    </option>
                  </select>
                </div>
                {tls === "verify-full" && (
                  <div className="field">
                    <label htmlFor="root-cert">
                      Certificado CA en el servidor
                    </label>
                    <input
                      id="root-cert"
                      name="root_cert"
                      placeholder="/ruta/al/certificado.crt"
                      required
                    />
                    <p className="field-help">
                      Ruta accesible por el proceso de TgStrm.
                    </p>
                  </div>
                )}
                <div className="database-actions">
                  <button
                    className="secondary-button"
                    type="submit"
                    value="test"
                  >
                    Probar conexión
                  </button>
                  <button className="submit-button" type="submit" value="save">
                    {busy ? "Procesando…" : "Guardar"}
                    {!busy && <ArrowRight size={16} />}
                  </button>
                </div>
              </fieldset>
            </form>
          )}
          {message && (
            <p role={error ? "alert" : "status"} className="notice">
              {message}
            </p>
          )}
        </section>
      </main>
      <footer>
        Configuración local · Los secretos se guardan cifrados en el servidor.
      </footer>
    </div>
  );
}
