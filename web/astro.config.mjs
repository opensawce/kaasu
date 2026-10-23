import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// SITE_URL and BASE_PATH are set by CI. Locally the site is served from "/".
export default defineConfig({
  site: process.env.SITE_URL ?? 'https://kaasu.opensawce.com',
  base: process.env.BASE_PATH ?? '/',
  integrations: [
    starlight({
      title: 'Kaasu',
      description: 'Documentation for Kaasu, open-source UPI payment confirmation for small shops.',
      logo: { src: './src/assets/kaasu-mark.svg', alt: 'Kaasu' },
      favicon: '/favicon.svg',
      social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/opensawce/kaasu' }],
      editLink: { baseUrl: 'https://github.com/opensawce/kaasu/edit/main/web/' },
      lastUpdated: true,
      head: [
        { tag: 'meta', attrs: { property: 'og:image', content: 'https://kaasu.opensawce.com/img/social-preview.png' } },
        { tag: 'meta', attrs: { property: 'og:site_name', content: 'Kaasu' } },
        { tag: 'meta', attrs: { property: 'og:locale', content: 'en_IN' } },
        { tag: 'meta', attrs: { name: 'twitter:image', content: 'https://kaasu.opensawce.com/img/social-preview.png' } },
        { tag: 'meta', attrs: { name: 'theme-color', content: '#0F7B5F' } },
        { tag: 'link', attrs: { rel: 'alternate', type: 'text/plain', href: '/llms.txt', title: 'LLM summary' } },
      ],
      customCss: [
        '@fontsource-variable/bricolage-grotesque',
        '@fontsource/ibm-plex-sans/400.css',
        '@fontsource/ibm-plex-sans/600.css',
        '@fontsource/ibm-plex-mono/400.css',
        './src/styles/tokens.css',
        './src/styles/starlight.css',
      ],
      // Add a Markdown file to a folder and it shows up here by itself.
      sidebar: [
        { label: 'Start', items: [{ autogenerate: { directory: 'docs/start' } }] },
        { label: 'Use Kaasu', items: [{ autogenerate: { directory: 'docs/use' } }] },
        { label: 'Connect', items: [{ autogenerate: { directory: 'docs/connect' } }] },
        { label: 'Reference', items: [{ autogenerate: { directory: 'docs/reference' } }] },
      ],
    }),
  ],
});
