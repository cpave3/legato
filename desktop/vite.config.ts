/// <reference types="vitest/config" />
import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"
import path from "path"

// The shared voice widget components live in the web UI (web/src/voice/);
// the voice window (voice.html) imports them through @web-voice. React is
// this app's own copy, whichever folder imports it.
const webVoice = path.resolve(__dirname, "../web/src/voice")

export default defineConfig({
  plugins: [react(), tailwindcss()],
  clearScreen: false,
  resolve: {
    alias: { "@web-voice": webVoice },
    dedupe: ["react", "react-dom"],
  },
  server: {
    port: 5174,
    strictPort: true,
    fs: { allow: [".", webVoice, path.resolve(__dirname, "../web/src/hooks")] },
  },
  build: {
    rollupOptions: {
      input: { main: path.resolve(__dirname, "index.html"), voice: path.resolve(__dirname, "voice.html") },
    },
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
  },
})
