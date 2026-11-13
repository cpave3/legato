// One lint configuration for every app under packages/. `pnpm lint` here
// lints them all; a package's own `pnpm exec eslint .` finds this file too.
import js from "@eslint/js"
import globals from "globals"
import tseslint from "typescript-eslint"

export default tseslint.config(
  { ignores: ["**/dist", "**/coverage", "**/node_modules"] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["**/*.ts", "**/*.mjs", "**/*.js"],
    languageOptions: { globals: { ...globals.browser, ...globals.node } },
  },
)
