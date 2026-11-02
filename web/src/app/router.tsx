import { createBrowserRouter, Link, redirect } from "react-router";
import { AccessPage } from "../features/auth/access-page";
import { AdminAccessPage } from "../features/auth/admin-access-page";

import { DatabasePage } from "../features/setup/database-page";
import { DashboardPage } from "../features/dashboard/dashboard-page";

type Destination = "root" | "database" | "login" | "home";
type SetupStatus = {
  database: "unconfigured" | "ready" | "recovery";
  telegram: "disconnected" | "authorized" | "connected";
};
type AdminSession = { authenticated: boolean; scope?: "full" | "password_change" };

async function adminSession(): Promise<AdminSession> {
  try { const response = await fetch("/api/v1/admin/session", { credentials: "same-origin" }); if (!response.ok) return { authenticated: false }; return response.json(); }
  catch { return { authenticated: false }; }
}

async function requireDestination(destination: Destination) {
  let status: SetupStatus;
  try {
    const response = await fetch("/api/v1/setup/status", {
      credentials: "same-origin",
    });
    if (!response.ok) throw new Error();
    status = await response.json();
  } catch {
    if (destination === "root") throw redirect("/setup/database");
    return null;
  }

  const databaseReady = status.database === "ready";
  const admin = databaseReady ? await adminSession() : { authenticated: false };
  if (databaseReady && !admin.authenticated) throw redirect("/admin/login");
  if (admin.scope === "password_change") throw redirect("/admin/change-password");
  const telegramReady =
    status.telegram === "authorized" || status.telegram === "connected";
  const expected = !databaseReady
    ? "/setup/database"
    : telegramReady
      ? "/home"
      : "/login";

  if (destination === "root") throw redirect(expected);
  if (destination === "database" && databaseReady) throw redirect(expected);
  if (destination === "login" && !databaseReady) {
    throw redirect("/setup/database");
  }
  if (destination === "login" && telegramReady) throw redirect("/home");
  if (destination === "home" && expected !== "/home") {
    throw redirect(expected);
  }
  return null;
}

export const router = createBrowserRouter([
  { path: "/", loader: () => requireDestination("root") },
  { path: "/admin/login", loader: async () => { const session=await adminSession(); if(session.authenticated) throw redirect(session.scope === "password_change" ? "/admin/change-password" : "/home"); return null; }, element: <AdminAccessPage /> },
  { path: "/admin/change-password", loader: async () => { const session=await adminSession(); if(!session.authenticated) throw redirect("/admin/login"); if(session.scope!=="password_change") throw redirect("/home"); return null; }, element: <AdminAccessPage mode="initial" /> },
  { path: "/admin/recovery", loader: async () => { const session=await adminSession(); if(session.authenticated && session.scope!=="password_change") throw redirect("/home"); return null; }, element: <AdminAccessPage mode="recovery" /> },
  {
    path: "/login",
    loader: () => requireDestination("login"),
    element: <AccessPage />,
  },
  {
    path: "/setup/database",
    loader: () => requireDestination("database"),
    element: <DatabasePage />,
  },
  {
    path: "/home",
    loader: () => requireDestination("home"),
    element: <DashboardPage />,
  },
  ...["/library", "/library/manage", "/library/topics/:topicId", "/library/topics/:topicId/manage", "/library/topics/:topicId/videos/:videoId"].map((path) => ({
    path,
    loader: async ({ request, params }: { request: Request; params: Record<string,string | undefined> }) => {
      await requireDestination("home");
      if (params.topicId) {
        const response = await fetch(`/api/v1/library/topics/${params.topicId}/location`, {credentials:"same-origin",signal:request.signal});
        if (!response.ok) throw new Response("El topic no está disponible", {status:response.status});
        const location = await response.json() as {group_id:string;collection_id:string};
        const base = location.collection_id ? `/library/collections/${location.collection_id}/groups/${location.group_id}` : `/groups/${location.group_id}`;
        const suffix = params.videoId ? `/videos/${params.videoId}` : new URL(request.url).pathname.endsWith("/manage") ? "/manage" : "";
        throw redirect(`${base}/topics/${params.topicId}${suffix}`);
      }
      const scopeResponse = await fetch("/api/v1/library/target", { credentials: "same-origin", signal: request.signal });
      if (!scopeResponse.ok) throw new Response("No se pudo cargar la biblioteca", { status: scopeResponse.status });
      const scope = await scopeResponse.json() as { mode: string; collection_id?: string; group_id?: string };
      if (scope.mode === "collection" && scope.collection_id) throw redirect(`/library/collections/${scope.collection_id}${new URL(request.url).pathname.endsWith("/manage") ? "/manage" : ""}`);
      return { libraryGroupId: scope.group_id };
    },
    element: <DashboardPage mode="library" />,
  })),
  ...["/library/collections/:collectionId", "/library/collections/:collectionId/manage", "/library/collections/:collectionId/groups/:groupId", "/library/collections/:collectionId/groups/:groupId/manage", "/library/collections/:collectionId/groups/:groupId/topics/:topicId", "/library/collections/:collectionId/groups/:groupId/topics/:topicId/manage", "/library/collections/:collectionId/groups/:groupId/topics/:topicId/videos/:videoId"].map(path => ({
    path,
    loader: async ({ request, params }: { request: Request; params: Record<string, string | undefined> }) => {
      await requireDestination("home");
      const response = await fetch(`/api/v1/library/collections/${params.collectionId}`, { credentials: "same-origin", signal: request.signal });
      if (!response.ok) throw new Response("La carpeta no está disponible", { status: response.status });
      const data = await response.json() as { groups: { group_id: string; state: string }[] };
      if (params.groupId && !data.groups.some(group => group.group_id === params.groupId && group.state !== "destination_missing")) throw new Response("El cajón no pertenece a esta carpeta", { status: 404 });
      return { libraryGroupId: params.groupId };
    },
    element: <DashboardPage mode="library" />,
  })),
  { path: "/imports/replications/:planId/review", loader: () => requireDestination("home"), element: <DashboardPage mode="replication-review" /> },
  {
    path: "/settings",
    loader: () => requireDestination("home"),
    element: <DashboardPage mode="settings" />,
  },
  {
    path: "/activity",
    loader: () => requireDestination("home"),
    element: <DashboardPage mode="activity" />,
  },
  {
    path: "/mirror",
    loader: () => requireDestination("home"),
    element: <DashboardPage mode="mirror" />,
  },
  {
    path: "/mirror/manage",
    loader: () => requireDestination("home"),
    element: <DashboardPage mode="mirror-manage" />,
  },
  {
    path: "/groups/:groupId",
    loader: () => requireDestination("home"),
    element: <DashboardPage />,
  },
  {
    path: "/groups/:groupId/manage",
    loader: () => requireDestination("home"),
    element: <DashboardPage mode="manage" />,
  },
  {
    path: "/groups/:groupId/import/review/:topicIds",
    loader: () => requireDestination("home"),
    element: <DashboardPage mode="import" />,
  },
  {
    path: "/groups/:groupId/import/review",
    loader: () => requireDestination("home"),
    element: <DashboardPage mode="import" />,
  },
  {
    path: "/groups/:groupId/topics/:topicId",
    loader: () => requireDestination("home"),
    element: <DashboardPage />,
  },
  {
    path: "/groups/:groupId/topics/:topicId/manage",
    loader: () => requireDestination("home"),
    element: <DashboardPage mode="manage" />,
  },
  {
    path: "/groups/:groupId/topics/:topicId/videos/:videoId",
    loader: () => requireDestination("home"),
    element: <DashboardPage />,
  },
  { path: "/dashboard", loader: () => redirect("/home") },
  {
    path: "*",
    element: (
      <main className="not-found">
        <h1>Esta página no existe.</h1>
        <Link to="/">Volver al inicio</Link>
      </main>
    ),
  },
]);
