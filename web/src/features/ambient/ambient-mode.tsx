import {
  exitOwnedFullscreen,
  ownsFullscreen,
  supportsFullscreen,
} from "./fullscreen";
import {
  AmbientBackdrop,
  type AmbientTitle as Title,
} from "./ambient-backdrop";
import { Dialog, DialogPanel, DialogTitle } from "@headlessui/react";
import { Maximize, Minimize, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

type Settings = {
  ambient_enabled?: boolean;
  ambient_idle_minutes?: number;
  ambient_window?: string;
  tmdb_configured?: boolean;
};
export function AmbientMode() {
  const [settings, setSettings] = useState<Settings>({});
  const [titles, setTitles] = useState<Title[]>([]);
  const [open, setOpen] = useState(false);
  const [index, setIndex] = useState(0);
  const [displayedTitle, setDisplayedTitle] = useState<Title | null>(null);
  const [fullscreen, setFullscreen] = useState(false);
  const [error, setError] = useState("");
  const panel = useRef<HTMLDivElement>(null);
  const last = useRef(Date.now());
  const started = useRef(0);
  const loading = useRef(false);
  const retryAt = useRef(0);
  const failedImages = useRef(new Set<string>());
  const fullscreenChangedAt = useRef(0);
  const dismiss = useCallback(() => {
    void exitOwnedFullscreen(document, panel.current).catch(() => {});
    setOpen(false);
    last.current = Date.now();
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    const load = () => {
      void fetch("/api/v1/settings/media", {
        signal: controller.signal,
        credentials: "same-origin",
      })
        .then((r) => (r.ok ? r.json() : {}))
        .then(setSettings)
        .catch(() => {});
    };
    load();
    window.addEventListener("tgstrm:media-settings-saved", load);
    return () => {
      controller.abort();
      window.removeEventListener("tgstrm:media-settings-saved", load);
    };
  }, []);
  useEffect(() => {
    if (settings.ambient_enabled === false) dismiss();
  }, [settings.ambient_enabled, dismiss]);
  useEffect(() => {
    const controller = new AbortController();
    const reset = () => {
      last.current = Date.now();
    };
    const events = [
      "pointermove",
      "pointerdown",
      "keydown",
      "wheel",
      "touchstart",
      "scroll",
    ];
    events.forEach((event) =>
      window.addEventListener(event, reset, { passive: true, capture: true }),
    );
    document.addEventListener("visibilitychange", reset);
    const activate = (preview = false) => {
      if (!settings.tmdb_configured || open || loading.current) return;
      if (!preview) {
        if (
          open ||
          loading.current ||
          !settings.ambient_enabled ||
          !settings.tmdb_configured ||
          document.hidden ||
          Date.now() < retryAt.current
        )
          return;
        const focused = document.activeElement;
        if (
          document.querySelector('[role="dialog"], [aria-busy="true"]') ||
          document.querySelector("form") ||
          focused?.matches('input,textarea,select,[contenteditable="true"]') ||
          [...document.querySelectorAll("video,audio")].some(
            (media) => !(media as HTMLMediaElement).paused,
          )
        ) {
          reset();
          return;
        }
        if (
          Date.now() - last.current <
          (settings.ambient_idle_minutes || 5) * 60000
        )
          return;
      }
      loading.current = true;
      void fetch(
        `/api/v1/ambient/trending?window=${settings.ambient_window || "week"}`,
        { signal: controller.signal, credentials: "same-origin" },
      )
        .then(async (r) => {
          if (!r.ok) throw Error();
          return r.json();
        })
        .then((data) => {
          if (
            controller.signal.aborted ||
            (!preview &&
              Date.now() - last.current <
                (settings.ambient_idle_minutes || 5) * 60000)
          )
            return;
          if (!data.titles?.length) {
            if (preview)
              window.dispatchEvent(
                new CustomEvent("tgstrm:ambient-preview-failed", {
                  detail:
                    "TMDB no devolvió tendencias con imágenes disponibles.",
                }),
              );
            retryAt.current = Date.now() + 60000;
            return;
          }
          failedImages.current.clear();
          setTitles(data.titles);
          setIndex(0);
          setDisplayedTitle(null);
          started.current = Date.now();
          setError("");
          setOpen(true);
        })
        .catch(() => {
          if (preview && !controller.signal.aborted)
            window.dispatchEvent(
              new CustomEvent("tgstrm:ambient-preview-failed", {
                detail:
                  "No se pudieron cargar las tendencias. Comprueba la conexión y la credencial de TMDB.",
              }),
            );
          retryAt.current = Date.now() + 60000;
        })
        .finally(() => {
          loading.current = false;
        });
    };
    const preview = () => activate(true);
    window.addEventListener("tgstrm:ambient-preview", preview);
    const timer = window.setInterval(() => activate(), 1000);
    return () => {
      window.removeEventListener("tgstrm:ambient-preview", preview);
      controller.abort();
      clearInterval(timer);
      events.forEach((event) => window.removeEventListener(event, reset, true));
      document.removeEventListener("visibilitychange", reset);
    };
  }, [settings, open, dismiss]);
  useEffect(() => {
    if (!open) return;
    const timer = window.setInterval(() => {
      if (!document.hidden) setIndex((i) => (i + 1) % titles.length);
    }, 12000);
    const changed = () => {
      fullscreenChangedAt.current = Date.now();
      setFullscreen(ownsFullscreen(document, panel.current));
    };
    document.addEventListener("fullscreenchange", changed);
    return () => {
      clearInterval(timer);
      document.removeEventListener("fullscreenchange", changed);
    };
  }, [open, titles.length]);
  const nextTitle = titles[index];
  const title = displayedTitle;
  useEffect(() => {
    if (!open || !titles.length) return;
    const next = new Image();
    next.src = `https://image.tmdb.org/t/p/w1280${titles[(index + 1) % titles.length].backdrop_path}`;
  }, [open, index, titles]);
  const toggleFullscreen = async () => {
    try {
      setError("");
      if (!supportsFullscreen(document, panel.current)) return;
      if (ownsFullscreen(document, panel.current))
        await exitOwnedFullscreen(document, panel.current);
      else await panel.current?.requestFullscreen();
    } catch {
      setError("Este navegador no ha permitido activar la pantalla completa.");
    }
  };
  return (
    <Dialog
      open={open}
      onClose={() => {
        if (ownsFullscreen(document, panel.current)) {
          void exitOwnedFullscreen(document, panel.current).catch(() => {});
          return;
        }
        if (Date.now() - fullscreenChangedAt.current > 500) dismiss();
      }}
      className="ambient-dialog"
    >
      <DialogPanel
        ref={panel}
        className="ambient-panel"
        onClick={(event) => {
          if (!(event.target as Element).closest("button")) dismiss();
        }}
        onPointerMove={(event) => {
          if (
            event.pointerType === "mouse" &&
            Date.now() - started.current > 2000 &&
            !document.fullscreenElement &&
            !(event.target as Element).closest(".ambient-controls")
          )
            dismiss();
        }}
      >
        {open && nextTitle && (
          <AmbientBackdrop
            title={nextTitle}
            onReady={setDisplayedTitle}
            onFailure={(failed) => {
              failedImages.current.add(`${failed.media_type}-${failed.id}`);
              if (failedImages.current.size >= titles.length) {
                retryAt.current = Date.now() + 60000;
                dismiss();
              } else setIndex((i) => (i + 1) % titles.length);
            }}
          />
        )}
        <DialogTitle className="ambient-accessible-title">
          {title?.title || title?.name || "Modo ambiente"}
        </DialogTitle>
        <div
          className="ambient-controls"
          onPointerMove={() => {
            started.current = Date.now();
          }}
        >
          {supportsFullscreen(document, panel.current) && (
            <button type="button" onClick={() => void toggleFullscreen()}>
              {fullscreen ? <Minimize size={17} /> : <Maximize size={17} />}
              <span>
                {fullscreen
                  ? "Salir de pantalla completa"
                  : "Pantalla completa"}
              </span>
            </button>
          )}
          <button type="button" onClick={dismiss}>
            <X size={17} />
            <span>Volver a TgStrm</span>
          </button>
        </div>
        {error && (
          <p className="ambient-error" role="status">
            {error}
          </p>
        )}
      </DialogPanel>
    </Dialog>
  );
}
