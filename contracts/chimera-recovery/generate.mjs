// Run: node contracts/chimera-recovery/generate.mjs /path/to/chimera
import { mkdtemp, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { pathToFileURL } from 'node:url'
const source = resolve(process.argv[2])
const { Compactor } = await import(pathToFileURL(join(source, 'packages/compaction/dist/index.js')))
const home = await mkdtemp(join(tmpdir(), 'recovery-excerpt-'))
const messages = [
  { role: 'user', content: 'Fix the build.\nKeep the existing theme semantics.' },
  { role: 'assistant', content: [
    { type: 'text', text: 'I will inspect the failing test.' },
    { type: 'tool-call', toolCallId: 'read-1', toolName: 'read', input: { path: 'src/build.ts' } },
  ] },
  { role: 'tool', content: [{ type: 'tool-result', toolCallId: 'read-1', toolName: 'read', output: { type: 'text', value: 'export const build = () => true\n' } }] },
  { role: 'assistant', content: 'The test expects the build to return true.\nNext: run the focused regression.' },
]
async function recover(input) {
  const session = { id: 'recovery-fixture', model: { providerId: 'fixture', modelId: 'unavailable' }, messages: structuredClone(input), fileOps: { reads: new Set(['src/build.ts']), writes: new Set() }, tasks: [{ content: 'Run the regression', status: 'in_progress' }] }
  const compactor = new Compactor({ home, contextWindow: 16000, config: { enabled: true, reserveTokens: 2000, keepRecentTokens: 0 }, resolveModel: async () => { throw new Error('Fixture summary model unavailable') } })
  return await compactor.compact(session, 'manual')
}
try {
  const full = await recover(messages)
  const truncated = await recover([messages[0], { role: 'user', content: 'Older diagnostic output.\n'.repeat(500) }, ...messages.slice(1)])
  await writeFile(new URL('./fixture.json', import.meta.url), JSON.stringify({ full, truncated }, null, 2) + '\n')
} finally { await rm(home, { recursive: true, force: true }) }
