import { useEffect, useRef, useState } from "react";

export type AmbientTitle = {
  id: number;
  title: string;
  name: string;
  overview: string;
  backdrop_path: string;
  poster_path: string;
  media_type: string;
};

const imageURL = (title: AmbientTitle) =>
  `https://image.tmdb.org/t/p/w1280${title.backdrop_path}`;

// Keep two stable image elements: the opaque current image stays underneath
// until the incoming image has decoded and completed its opacity transition.
export function AmbientBackdrop({
  title,
  onReady,
  onFailure,
}: {
  title: AmbientTitle;
  onReady: (title: AmbientTitle) => void;
  onFailure: (title: AmbientTitle) => void;
}) {
  const [slots, setSlots] = useState<(AmbientTitle | null)[]>([null, null]);
  const [active, setActive] = useState(0);
  const [incoming, setIncoming] = useState<number | null>(null);
  const [revealed, setRevealed] = useState(false);
  const activeRef = useRef(0);
  const frames = useRef<number[]>([]);
  const finishTimer = useRef<number | undefined>(undefined);
  const sequence = useRef(0);
  const callbacks = useRef({ onReady, onFailure });
  callbacks.current = { onReady, onFailure };

  useEffect(() => {
    const version = ++sequence.current;
    let cancelled = false;
    const image = new Image();
    const loaded = new Promise<void>((resolve, reject) => {
      image.onload = () => resolve();
      image.onerror = () => reject(new Error("Image unavailable"));
    });
    image.src = imageURL(title);
    void loaded
      .then(async () => {
        if (image.decode) await image.decode();
        if (cancelled) return;
        const slot = 1 - activeRef.current;
        setRevealed(false);
        setSlots((previous) =>
          previous.map((value, index) => (index === slot ? title : value)),
        );
        setIncoming(slot);
      })
      .catch(() => {
        if (!cancelled) callbacks.current.onFailure(title);
      });
    return () => {
      cancelled = true;
      if (sequence.current === version) ++sequence.current;
      frames.current.forEach(cancelAnimationFrame);
      frames.current = [];
      clearTimeout(finishTimer.current);
    };
  }, [title]);

  const reveal = async (element: HTMLImageElement, slot: number) => {
    const version = sequence.current;
    try {
      if (element.decode) await element.decode();
      if (version !== sequence.current || slot !== incoming) return;
      frames.current.push(
        requestAnimationFrame(() => {
          frames.current.push(
            requestAnimationFrame(() => {
              if (version !== sequence.current) return;
              setRevealed(true);
              callbacks.current.onReady(title);
              // Also completes for reduced motion and browsers without transitionend.
              finishTimer.current = window.setTimeout(
                () => {
                  if (version !== sequence.current) return;
                  activeRef.current = slot;
                  setActive(slot);
                  setIncoming(null);
                },
                matchMedia("(prefers-reduced-motion: reduce)").matches
                  ? 0
                  : 1600,
              );
            }),
          );
        }),
      );
    } catch {
      if (version === sequence.current) callbacks.current.onFailure(title);
    }
  };

  return (
    <div className="ambient-backdrops" aria-hidden="true">
      {slots.map((value, slot) => (
        <div
          key={slot}
          className={`ambient-slide${slot === active ? " ambient-slide-current" : ""}${slot === incoming ? " ambient-slide-incoming" : ""}${slot === incoming && revealed ? " ambient-slide-revealed" : ""}`}
        >
          <img
            className="ambient-background"
            src={value ? imageURL(value) : undefined}
            alt=""
            onLoad={(event) => {
              if (slot === incoming) void reveal(event.currentTarget, slot);
            }}
            onError={() => {
              if (value && slot === incoming)
                callbacks.current.onFailure(value);
            }}
          />
          <div className="ambient-shade" />
          {value && (
            <div className="ambient-copy">
              <h2>{value.title || value.name}</h2>
              <p className="ambient-overview">{value.overview}</p>
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
