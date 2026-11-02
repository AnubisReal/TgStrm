export type LibraryTarget = { mode: "group" | "collection"; group_id?: string; collection_id?: string; revision: number };
export type Collection = { id: string; name: string; state: string; telegram_filter_id: number; revision: number; series_name_format: string; movies_name_format: string; group_count: number };
export type CollectionMember = { id: string; group_id: string; title: string; bucket: string; media_kind: string; part: number; state: string; manual: boolean };
export function libraryRequestKey(): string {
  if (typeof crypto.randomUUID === "function") return crypto.randomUUID();
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 15) | 64;
  bytes[8] = (bytes[8] & 63) | 128;
  const hex = Array.from(bytes, byte => byte.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
export async function libraryRequest<T>(url: string, body?: unknown, method = "GET", signal?: AbortSignal): Promise<T> {
  const response = await fetch(url, { credentials: "same-origin", method, signal, headers: body === undefined ? undefined : { "Content-Type": "application/json" }, body: body === undefined ? undefined : JSON.stringify(body) });
  const data = await response.json();
  if (!response.ok) throw new Error(data.message || "No se pudo completar la operación.");
  return data as T;
}
