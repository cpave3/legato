import js from "@eslint/js"
import globals from "globals"
import tseslint from "typescript-eslint"

export default tseslint.config(
  { ignores: ["dist", "coverage"] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["src/**/*.ts", "vite.config.ts"],
    languageOptions: { globals: { ...globals.browser, ...globals.node, chrome: "readonly" } },
  },
)
