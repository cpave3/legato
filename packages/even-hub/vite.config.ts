import { fileURLToPath } from "node:url"
import { defineConfig, loadEnv } from "vite"
import { fakePhone, legatoDev } from "./dev/legato-dev.ts"

const root = fileURLToPath(new URL(".", import.meta.url))

export default defineConfig(({ command, mode }) => ({
  // The dev server is a fake phone (dev/legato-dev.ts). LEGATO_FIXTURE comes
  // from the environment or a gitignored .env.local.
  plugins:
    command === "serve"
      ? [legatoDev(fakePhone(loadEnv(mode, root, "LEGATO_"), root), loadEnv(mode, root, "VITE_").VITE_LEGATO_PHONE === "real")]
      : [],
  build: { target: "es2022" },
}))
