import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
export default defineConfig({
  plugins: [vue()],
  build: { outDir: "../web/mall/dist", emptyOutDir: true },
  server: {
    proxy: { "/api": { target: "http://127.0.0.1:8088", changeOrigin: false } },
  },
});
