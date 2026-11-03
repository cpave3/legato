// The Legato documentation site, published to GitHub Pages by
// .github/workflows/docs.yml. Pages are Markdown under src/content/docs;
// each sidebar group lists the folder of the same name.
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightLinksValidator from 'starlight-links-validator';
import { writeGlossary } from './src/glossary.mjs';

writeGlossary();

const group = (label, directory) => ({ label, items: [{ autogenerate: { directory } }] });

export default defineConfig({
  site: 'https://cpave3.github.io',
  base: '/legato',
  integrations: [
    starlight({
      title: 'Legato',
      description:
        'A keyboard-driven board for tasks and the AI coding agents that work on them.',
      logo: { src: './src/assets/logo.png', alt: 'Legato' },
      favicon: '/favicon.ico',
      social: [
        { icon: 'github', label: 'GitHub', href: 'https://github.com/cpave3/legato' },
      ],
      editLink: {
        baseUrl: 'https://github.com/cpave3/legato/edit/main/site/',
      },
      lastUpdated: true,
      customCss: [
        '@fontsource-variable/geist',
        '@fontsource-variable/geist-mono',
        './src/styles/legato.css',
      ],
      // localhost links are deliberate: the web UI runs on your machine.
      plugins: [starlightLinksValidator({ errorOnLocalLinks: false })],
      sidebar: [
        group('Start here', 'start'),
        group('Ways to run Legato', 'running'),
        group('Using Legato', 'using'),
        group('Agent tools', 'agent-tools'),
        group('Integrations', 'integrations'),
        group('Security', 'security'),
        group('Reference', 'reference'),
        group('Contributing', 'contributing'),
      ],
    }),
  ],
});
