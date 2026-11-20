
import { resolve } from "node:path";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
export default defineConfig({
  base: "/canvas-app/",
  plugins: [react()],
  resolve: { alias: [
    {find:"@/components/model-picker",replacement:resolve(__dirname,"src/embedded/model-picker.tsx")},
    {find:"@/components/image-settings-panel",replacement:resolve(__dirname,"src/embedded/image-settings.ts")},
    {find:"@/components/video-settings-panel",replacement:resolve(__dirname,"src/embedded/video-settings.ts")},
    {find:"@/stores/use-config-store",replacement:resolve(__dirname,"src/embedded/config.ts")},
    {find:"@/services/api/prompts",replacement:resolve(__dirname,"src/embedded/prompts-api.ts")},
    {find:"@/lib/localforage-storage",replacement:resolve(__dirname,"src/embedded/localforage-storage.ts")},
    {find:"localforage",replacement:resolve(__dirname,"src/embedded/storage.ts")},
    ...["components/layout/client-root-init","components/layout/app-config-modal","components/agent/agent-panel","lib/canvas/plugin-loader","services/api/model-plugin"].map(name=>({find:"@/"+name,replacement:resolve(__dirname,"src/embedded/disabled.tsx")})),
    {find:/^\.\/model-plugin$/,replacement:resolve(__dirname,"src/embedded/disabled.tsx")},
    {find:"@/components/layout/user-status-actions",replacement:resolve(__dirname,"src/embedded/layout.tsx")},
    {find:"@",replacement:resolve(__dirname,"src")}
  ]},
  define: { __APP_VERSION__:JSON.stringify("v0.19.0 (new-api)"), __APP_RELEASES__:"[]" },
  build: { outDir: "dist", emptyOutDir: true }
});
