import { readFile } from "node:fs/promises"

const manifest = JSON.parse(await readFile(new URL("../dist/manifest.json", import.meta.url), "utf8"))
for (const declaration of manifest.content_scripts ?? []) {
  for (const script of declaration.js ?? []) {
    const source = await readFile(new URL(`../dist/${script}`, import.meta.url), "utf8")
    if (/^\s*(?:import|export)\s/m.test(source)) {
      throw new Error(`${script} contains ES module syntax, but Manifest V3 content scripts execute as classic scripts`)
    }
  }
}
