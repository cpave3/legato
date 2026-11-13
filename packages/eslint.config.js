// One lint configuration for every app under packages/. `pnpm lint` here
// lints them all; a package's own `pnpm exec eslint .` finds this file too.
// An app's src/ runs in a browser (the Even app's WebView, for even-hub) and
// gets the browser's globals; everything else runs in Node.
import js from "@eslint/js"
import globals from "globals"
import tseslint from "typescript-eslint"

export default tseslint.config(
  { ignores: ["**/dist", "**/coverage", "**/node_modules", "**/.pack"] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["*/src/**/*.ts"],
    languageOptions: { globals: globals.browser },
  },
  {
    files: ["**/*.{ts,mjs,js}"],
    ignores: ["*/src/**"],
    languageOptions: { globals: globals.node },
  },
)
