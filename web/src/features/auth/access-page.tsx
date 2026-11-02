import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { Link, Navigate, useNavigate } from "react-router";
import { ArrowLeft, ArrowRight, Eye, EyeOff } from "lucide-react";
import { Brand } from "../../components/ui/brand";

type Step =
  "loading" | "database" | "api" | "phone" | "code" | "password" | "authorized";
type AuthState = {
  step: Step;
  delivery?: string;
  name?: string;
  user_id?: string;
};

async function authRequest(
  path: string,
  body?: Record<string, unknown>,
): Promise<AuthState> {
  const response = await fetch(`/api/v1/telegram/auth${path}`, {
    method: body ? "POST" : "GET",
    credentials: "same-origin",
    headers: body ? { "Content-Type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await response.json();
  if (!response.ok) {
    if (data.code === "database_required") return { step: "database" };
    throw new Error(data.message || "No se pudo completar el acceso.");
  }
  return data;
}

export function AccessPage() {
  const navigate = useNavigate();
  const [state, setState] = useState<AuthState>({ step: "loading" });
  const [visible, setVisible] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [connectionAttempt, setConnectionAttempt] = useState(0);

  function editAPI() {
    setVisible(false);
    setError("");
    setState({ step: "api" });
  }

  useEffect(() => {
    let active = true;
    authRequest("")
      .then((next) => active && setState(next))
      .catch(
        (reason) =>
          active &&
          setError(
            reason instanceof Error
              ? reason.message
              : "No se pudo conectar con el servidor.",
          ),
      );
    return () => {
      active = false;
    };
  }, [connectionAttempt]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const values = new FormData(form);
    let path = "";
    let body: Record<string, unknown> = {};
    if (state.step === "api") {
      path = "/configure";
      body = {
        api_id: Number(values.get("api_id")),
        api_hash: values.get("api_hash"),
      };
    } else if (state.step === "phone") {
      path = "/send-code";
      body = { phone: String(values.get("phone")).replace(/[ ()-]/g, "") };
    } else if (state.step === "code") {
      path = "/verify-code";
      body = { code: String(values.get("code")).replace(/\s/g, "") };
    } else if (state.step === "password") {
      path = "/verify-password";
      body = { password: values.get("password") };
    }
    setBusy(true);
    setError("");
    try {
      const next = await authRequest(path, body);
      if (next.step === "authorized") {
        navigate("/home", { replace: true });
        return;
      }
      setState(next);
      form.reset();
    } catch (reason) {
      setError(
        reason instanceof Error
          ? reason.message
          : "No se pudo completar el acceso.",
      );
    } finally {
      setBusy(false);
    }
  }

  const descriptions: Partial<Record<Step, string>> = {
    api: "Primero, configura el acceso a la API.",
    phone: "Introduce el número de tu cuenta.",
    code: `Introduce el código enviado por ${state.delivery || "Telegram"}.`,
    password: "Tu cuenta utiliza verificación en dos pasos.",
  };

  return (
    <div className="access-page">
      <header className="site-header">
        <Brand />
        <span className="header-pill">Telegram</span>
      </header>
      <main className="access-main">
        <section className="auth-card" aria-labelledby="login-title">
          {state.step === "loading" && !error && (
            <p role="status">Comprobando tu sesión…</p>
          )}
          {state.step === "loading" && error && (
            <button
              type="button"
              className="submit-button"
              onClick={() => {
                setError("");
                setConnectionAttempt((attempt) => attempt + 1);
              }}
            >
              Reintentar conexión
            </button>
          )}
          {state.step === "database" && (
            <>
              <h1 id="login-title">Prepara el almacenamiento.</h1>
              <p className="description">
                TgStrm necesita PostgreSQL antes de guardar una sesión de
                Telegram.
              </p>
              <Link className="submit-button" to="/setup/database">
                Configurar PostgreSQL <ArrowRight size={17} />
              </Link>
            </>
          )}
          {state.step === "authorized" && <Navigate to="/home" replace />}
          {(["api", "phone", "code", "password"] as Step[]).includes(
            state.step,
          ) && (
            <>
              <div className="step-indicator" aria-label={`Paso ${state.step}`}>
                <span className="active" />
                <span className={state.step !== "api" ? "active" : ""} />
                <span
                  className={
                    state.step === "code" || state.step === "password"
                      ? "active"
                      : ""
                  }
                />
              </div>
              <h1 id="login-title">Conecta tu Telegram.</h1>
              <p className="description">{descriptions[state.step]}</p>
              <form key={state.step} onSubmit={submit}>
                <fieldset disabled={busy}>
                  {state.step === "api" && (
                    <>
                      <div className="field">
                        <label htmlFor="api-id">API ID</label>
                        <input
                          id="api-id"
                          name="api_id"
                          inputMode="numeric"
                          pattern="[0-9]+"
                          required
                          autoComplete="off"
                        />
                      </div>
                      <SecretField
                        id="api-hash"
                        name="api_hash"
                        label="API hash"
                        visible={visible}
                        setVisible={setVisible}
                        autoComplete="off"
                      />
                      <p className="field-help">
                        Disponibles en{" "}
                        <a
                          href="https://my.telegram.org/apps"
                          target="_blank"
                          rel="noreferrer"
                        >
                          my.telegram.org ↗
                        </a>
                      </p>
                    </>
                  )}
                  {state.step === "phone" && (
                    <div className="field">
                      <label htmlFor="phone">Número de teléfono</label>
                      <input
                        id="phone"
                        name="phone"
                        type="tel"
                        autoComplete="tel"
                        placeholder="+34 612 345 678"
                        required
                        pattern="\+[0-9 ()-]{7,20}"
                      />
                      <p className="field-help">
                        Incluye el prefijo de tu país.
                      </p>
                    </div>
                  )}
                  {state.step === "code" && (
                    <div className="field">
                      <label htmlFor="code">Código de verificación</label>
                      <input
                        id="code"
                        name="code"
                        inputMode="numeric"
                        autoComplete="one-time-code"
                        required
                        minLength={3}
                        maxLength={12}
                        autoFocus
                      />
                    </div>
                  )}
                  {state.step === "password" && (
                    <SecretField
                      id="password"
                      name="password"
                      label="Contraseña de verificación"
                      visible={visible}
                      setVisible={setVisible}
                      autoComplete="current-password"
                    />
                  )}
                  <button className="submit-button" type="submit">
                    {busy
                      ? "Procesando…"
                      : state.step === "api"
                        ? "Guardar y continuar"
                        : state.step === "phone"
                          ? "Enviar código"
                          : "Verificar"}
                    {!busy && <ArrowRight size={17} />}
                  </button>
                </fieldset>
              </form>
            </>
          )}
          {error && (
            <p className="notice" role="alert">
              {error}
            </p>
          )}
          {(["phone", "code", "password"] as Step[]).includes(state.step) && (
            <button className="back-button" type="button" onClick={editAPI}>
              <ArrowLeft size={14} /> Cambiar API ID
            </button>
          )}
          {state.step !== "database" && state.step !== "loading" && (
            <Link className="back-button quiet-link" to="/setup/database">
              <ArrowLeft size={14} />
              Almacenamiento
            </Link>
          )}
        </section>
      </main>
      <footer>
        Conexión protegida · Los códigos y la contraseña 2FA no se guardan.
      </footer>
    </div>
  );
}

function SecretField({
  id,
  name,
  label,
  visible,
  setVisible,
  autoComplete,
}: {
  id: string;
  name: string;
  label: string;
  visible: boolean;
  setVisible(value: boolean): void;
  autoComplete: string;
}) {
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <div className="password-wrap">
        <input
          id={id}
          name={name}
          type={visible ? "text" : "password"}
          required
          autoComplete={autoComplete}
          spellCheck={false}
        />
        <button
          className="reveal"
          type="button"
          aria-label={visible ? `Ocultar ${label}` : `Mostrar ${label}`}
          aria-pressed={visible}
          onClick={() => setVisible(!visible)}
        >
          {visible ? <EyeOff size={18} /> : <Eye size={18} />}
        </button>
      </div>
    </div>
  );
}
