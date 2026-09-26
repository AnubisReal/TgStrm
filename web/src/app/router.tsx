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
