import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { useNavigate } from "react-router";
import { ArrowRight, ChevronDown, Database, Search } from "lucide-react";
import { Brand } from "../../components/ui/brand";

type SetupState = "loading" | "unconfigured" | "ready" | "recovery" | "offline";
export function DatabasePage() {
  const navigate = useNavigate();
  const [state, setState] = useState<SetupState>("loading");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState(false);
  const [tls, setTLS] = useState("disable");
  const [runtimeMode, setRuntimeMode] = useState<
    "native" | "container" | "external"
  >("native");
  const [host, setHost] = useState("127.0.0.1");
  const [user, setUser] = useState("");
  const [databaseName, setDatabaseName] = useState("tgstrm");
  const [setupToken, setSetupToken] = useState("");
  const [createDatabase, setCreateDatabase] = useState(true);
  const [probing, setProbing] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    fetch("/api/v1/setup/status", { signal: controller.signal })
      .then(async (r) => {
        if (!r.ok) throw new Error();
        const d = await r.json();
        if (
          d.database_setup_mode === "container" ||
          d.database_setup_mode === "external"
        )
          setRuntimeMode(d.database_setup_mode);
        if (d.database_setup_mode === "container") {
          setHost("postgres");
          setUser("tgstrm");
        }
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
            create_database: createDatabase,
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
  async function probeLocal() {
    setProbing(true);
    setMessage("");
    setError(false);
    try {
      const response = await fetch("/api/v1/setup/database/probe-local", {
        method: "POST",
        headers: { "X-Setup-Token": setupToken },
      });
      const data = await response.json();
      if (!response.ok)
        throw new Error(data.message || "No se pudo comprobar PostgreSQL.");
      setMessage(data.message);
    } catch (e) {
      setError(true);
      setMessage(
        e instanceof Error ? e.message : "No se pudo comprobar PostgreSQL.",
      );
    } finally {
      setProbing(false);
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
                    value={setupToken}
                    onChange={(event) => setSetupToken(event.target.value)}
                  />
                  <p className="field-help">
                    Aparece en la terminal al iniciar TgStrm por primera vez.
                  </p>
                </div>
                {runtimeMode === "native" ? (
                  <div className="database-local-check">
                    <div>
                      <strong>PostgreSQL en este equipo</strong>
                      <p>
                        Comprueba si hay un servidor local escuchando en el
                        puerto habitual.
                      </p>
                    </div>
                    <button
                      className="secondary-button"
                      type="button"
                      onClick={probeLocal}
                      disabled={probing || !setupToken}
                    >
                      <Search size={15} />{" "}
                      {probing ? "Comprobando…" : "Comprobar"}
                    </button>
                    <p className="database-downloads">
                      Si falta, consulta la descarga oficial para{" "}
                      <a
                        href="https://www.postgresql.org/download/macos/"
                        target="_blank"
                        rel="noreferrer"
                      >
                        macOS
                      </a>
                      ,{" "}
                      <a
                        href="https://www.postgresql.org/download/windows/"
                        target="_blank"
                        rel="noreferrer"
                      >
                        Windows
                      </a>{" "}
                      o{" "}
                      <a
                        href="https://www.postgresql.org/download/linux/"
                        target="_blank"
                        rel="noreferrer"
                      >
                        Linux
                      </a>
                      . El instalador de macOS/Windows lo aloja EDB, según
                      informa PostgreSQL.
                    </p>
                  </div>
                ) : runtimeMode === "container" ? (
                  <div className="database-local-check">
                    <strong>PostgreSQL en Docker</strong>
                    <p>
                      Usa el servicio <code>postgres</code> de la misma red del
                      Stack. Introduce la contraseña definida en Portainer.
                    </p>
                  </div>
                ) : (
                  <div className="database-local-check">
                    <strong>PostgreSQL externo</strong>
                    <p>
                      Escribe el host y puerto que TgStrm puede alcanzar desde
                      el contenedor. PostgreSQL no se instala ni se sondea desde
                      el host en este modo.
                    </p>
                  </div>
                )}
                <div className="field-row">
                  <div className="field">
                    <label htmlFor="host">Servidor</label>
                    <input
                      id="host"
                      name="host"
                      value={host}
                      onChange={(event) => setHost(event.target.value)}
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
                    value={databaseName}
                    onChange={(event) => setDatabaseName(event.target.value)}
                    required
                    autoComplete="off"
                  />
                  <p className="field-help">
                    Se crea si falta, o puedes conectarte a una base que ya
                    exista.
                  </p>
                </div>
                <label className="database-create-option">
                  <input
                    type="checkbox"
                    checked={createDatabase}
                    onChange={(event) =>
                      setCreateDatabase(event.target.checked)
                    }
                  />
                  <span>
                    <strong>Crear la base si falta</strong>
                    <small>
                      Si falta, requiere CREATEDB y acceso al servidor de
                      mantenimiento postgres. Nunca se borra ni sustituye una
                      base existente.
                    </small>
                  </span>
                </label>
                <div className="field-row equal">
                  <div className="field">
                    <label htmlFor="user">Usuario</label>
                    <input
                      id="user"
                      name="user"
                      required
                      autoComplete="off"
                      value={user}
                      onChange={(event) => setUser(event.target.value)}
                    />
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
                  <span className="settings-select-control setup-tls-control">
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
                    <i aria-hidden="true">
                      <ChevronDown size={15} />
                    </i>
                  </span>
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
