// The glossary page is GLOSSARY.md at the root of the repository, which
// stays the one copy. Each build writes it into the docs as an ordinary
// page (git-ignored), so it is searched and listed like the others.
import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const source = new URL('../../GLOSSARY.md', import.meta.url);
const target = new URL('./content/docs/reference/glossary.md', import.meta.url);

export function glossaryPage(markdown) {
  // The file's own title ("# Legato") gives way to the page's.
  const body = markdown.replace(/^# .*\n+/, '');
  return [
    '---',
    'title: Glossary',
    'description: The words Legato uses, and the words it avoids.',
    'sidebar:',
    '  order: 90',
    'editUrl: https://github.com/cpave3/legato/edit/main/GLOSSARY.md',
    '---',
    '',
    '<!-- Written from GLOSSARY.md by src/glossary.mjs. Edit that file. -->',
    '',
    body,
  ].join('\n');
}

export function writeGlossary() {
  writeFileSync(fileURLToPath(target), glossaryPage(readFileSync(source, 'utf8')));
}
