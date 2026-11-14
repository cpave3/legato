import { fileURLToPath } from "node:url"
import { defineConfig, loadEnv } from "vite"
import { devSettings, legatoDev } from "./dev/legato-dev.ts"

const root = fileURLToPath(new URL(".", import.meta.url))

export default defineConfig(({ command, mode }) => ({
  // LEGATO_* come from the environment or a gitignored .env.local. They are
  // read here, in the dev server, and never reach the bundle (only VITE_*
  // variables would).
  plugins: command === "serve" ? [legatoDev(devSettings(loadEnv(mode, root, "LEGATO_"), root))] : [],
  build: { target: "es2022" },
}))
