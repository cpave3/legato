import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
const root=fileURLToPath(new URL('./',import.meta.url));
// Existing repo toolchain only; generated runtime has no framework dependency.
const tsc=process.env.TSC_PATH ?? fileURLToPath(new URL('../../../web/node_modules/typescript/bin/tsc',import.meta.url));
execFileSync(process.execPath,[tsc,'--target','ES2022','--module','ES2022','--lib','ES2022,DOM','--strict','--outDir',root+'.build',root+'core.ts',root+'ui.ts'],{stdio:'inherit'});
