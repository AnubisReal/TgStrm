// Safari/iOS can expose no Fullscreen API at all. Never compare empty refs
// with a missing/fullscreen-null property or call an unavailable method.
export function ownsFullscreen(
  doc: Document,
  element: Element | null,
): boolean {
  return element !== null && doc.fullscreenElement === element;
}

export function supportsFullscreen(
  doc: Document,
  element: Element | null,
): boolean {
  return (
    doc.fullscreenEnabled === true &&
    typeof doc.exitFullscreen === "function" &&
    typeof element?.requestFullscreen === "function"
  );
}

export async function exitOwnedFullscreen(
  doc: Document,
  element: Element | null,
): Promise<void> {
  if (
    ownsFullscreen(doc, element) &&
    typeof doc.exitFullscreen === "function"
  ) {
    await doc.exitFullscreen();
  }
}
