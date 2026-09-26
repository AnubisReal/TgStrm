import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, "", "TGSTRM_");
  const allowedHosts = Array.from(new Set([
    "localhost",
    "127.0.0.1",
    ...(env.TGSTRM_ALLOWED_HOSTS || "").split(",").map((host) => host.trim()).filter(Boolean),
  ]));

  return {
    plugins: [react()],
    server: {
      host: "0.0.0.0",
      port: 5173,
      strictPort: true,
      allowedHosts,
      proxy: {
        "/api": { target: "http://127.0.0.1:5151", ws: true },
        "/stream": { target: "http://127.0.0.1:5151" },
      },
    },
    preview: {
      host: "0.0.0.0",
      port: 5173,
      strictPort: true,
      allowedHosts,
    },
  };
});
