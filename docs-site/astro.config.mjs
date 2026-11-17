import starlight from '@astrojs/starlight';
import { defineConfig } from 'astro/config';
import mermaid from 'astro-mermaid';
import starlightGitHubAlerts from 'starlight-github-alerts';
import starlightLinksValidator from 'starlight-links-validator';
import starlightLlmsTxt from 'starlight-llms-txt';
import starlightSidebarTopics from 'starlight-sidebar-topics';
import { readFileSync } from 'node:fs';

const versions = JSON.parse(readFileSync(new URL('./src/generated/versions.json', import.meta.url), 'utf8'));

// One current channel. Groups follow the learning / concepts / reference / project
// order used by the sibling documentation site, scoped to these pages.
const sidebarTemplate = [
  {
    label: 'Getting started',
    items: [
      { label: 'Home', slug: 'index' },
      { label: 'Quick start', slug: 'getting-started' },
    ],
  },
  {
    label: 'Concepts',
    items: [
      { label: 'How it works', slug: 'how-it-works' },
      { label: 'Architecture', slug: 'architecture' },
      { label: 'Governed AI agents', slug: 'ai-agents' },
    ],
  },
  {
    label: 'Reference',
    items: [
      { label: 'Packaging profiles', slug: 'packaging' },
    ],
  },
  {
    label: 'Project',
    items: [
      { label: 'Roadmap', slug: 'roadmap' },
    ],
  },
];

function versionItems(version) {
  const route = (slug) => version.prefix ? (slug === 'index' ? version.prefix : `${version.prefix}/${slug}`) : slug;
  const versionEntries = (entries) => entries.flatMap((item) => {
    if (item.slug) return version.pages.includes(item.slug) ? [{ ...item, slug: route(item.slug) }] : [];
    if (item.items) {
      const items = versionEntries(item.items);
      return items.length ? [{ ...item, collapsed: true, items }] : [];
    }
    const directory = item.autogenerate.directory;
    return version.pages.some((page) => page.startsWith(`${directory}/`))
      ? [{ autogenerate: { directory: route(directory) } }] : [];
  });
  return versionEntries(sidebarTemplate);
}

const topics = versions.map((version) => ({
  id: version.id,
  label: version.label,
  link: `/conduit/${version.prefix ? version.prefix + '/' : ''}`,
  items: versionItems(version),
}));

// Project site: https://jonathanagustin.github.io/conduit/
// `site` is the origin. `base` is the repository path GitHub Pages serves.
export default defineConfig({
  site: 'https://jonathanagustin.github.io',
  base: '/conduit',
  trailingSlash: 'always',
  integrations: [
    mermaid({ theme: 'default', autoTheme: true }),
    starlight({
      title: 'Conduit',
      description:
        'Self-hosted delivery control plane that takes an upstream app from source to a governed UDS package.',
      logo: {
        light: './src/assets/conduit-wordmark-light.svg',
        dark: './src/assets/conduit-wordmark-dark.svg',
        replacesTitle: true,
      },
      favicon: 'favicon.svg',
      head: [
        {
          tag: 'meta',
          attrs: {
            property: 'og:image',
            content: 'https://jonathanagustin.github.io/conduit/demo/conduit-excalidraw-demo.webp',
          },
        },
        {
          tag: 'meta',
          attrs: { property: 'og:image:alt', content: 'Conduit console walkthrough' },
        },
        {
          tag: 'meta',
          attrs: { name: 'twitter:card', content: 'summary_large_image' },
        },
        {
          tag: 'meta',
          attrs: {
            name: 'twitter:image',
            content: 'https://jonathanagustin.github.io/conduit/demo/conduit-excalidraw-demo.webp',
          },
        },
      ],
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/jonathanagustin/conduit',
        },
      ],
      editLink: {
        baseUrl: 'https://github.com/jonathanagustin/conduit/edit/main/docs-site/',
      },
      lastUpdated: true,
      customCss: ['./src/styles/custom.css'],
      components: {
        Hero: './src/components/Hero.astro',
        DraftContentNotice: './src/components/DraftContentNotice.astro',
        ThemeSelect: './src/components/ThemeSelect.astro',
        MarkdownContent: './src/components/MarkdownContent.astro',
        Search: './src/components/Search.astro',
      },
      expressiveCode: { shiki: { langAlias: { logql: 'txt' } } },
      plugins: [
        starlightSidebarTopics(topics, { exclude: ['/404'] }),
        starlightGitHubAlerts(),
        starlightLinksValidator(),
        starlightLlmsTxt({
          promote: ['index*', 'getting-started*'],
        }),
      ],
    }),
  ],
});
