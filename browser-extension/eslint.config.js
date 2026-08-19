import js from "@eslint/js"
import globals from "globals"
import tseslint from "typescript-eslint"

export default tseslint.config(
  { ignores: ["dist", "coverage"] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["src/**/*.ts", "vite.config.ts", "vite.content.config.ts", "scripts/**/*.mjs"],
    languageOptions: { globals: { ...globals.browser, ...globals.node, chrome: "readonly" } },
  },
)
