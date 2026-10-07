import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// 빌드 결과는 Go 바이너리가 내장하는 internal/webui/dist로 간다.
// public/.keep이 빌드 때마다 dist/.keep을 되살려, 저장소에서 .keep이 지워진 것처럼 보이지 않게 한다.
export default defineConfig({
  plugins: [react()],
  build: { outDir: "../internal/webui/dist", emptyOutDir: true },
  // npm run dev: API는 로컬에서 띄운 ssl-checker serve(:8080)로 보낸다.
  server: { proxy: { "/api": "http://localhost:8080" } },
});
