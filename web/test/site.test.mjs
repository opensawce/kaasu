// Checks the built site in dist/. Run with `npm test` (builds first). Offline, no dependencies.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync, statSync, existsSync } from 'node:fs';
import { join, relative } from 'node:path';

const root = new URL('..', import.meta.url).pathname;
const dist = join(root, 'dist');
const SITE = 'https://kaasu.opensawce.com';

const walk = (dir) => readdirSync(dir).flatMap((f) => {
  const p = join(dir, f);
  return statSync(p).isDirectory() ? walk(p) : [p];
});
const pages = walk(dist).filter((f) => f.endsWith('.html') && !f.includes('/pagefind/') && !f.endsWith('404.html'));
const read = (f) => readFileSync(f, 'utf8');
const text = (html) => html.replace(/<(script|style)[\s\S]*?<\/\1>/g, '').replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');
const name = (f) => '/' + relative(dist, f).replace(/index\.html$/, '');
const home = read(join(dist, 'index.html'));

test('site builds the expected pages', () => {
  for (const p of ['/', '/terms/', '/docs/', '/docs/start/install/', '/docs/reference/security/']) {
    assert.ok(pages.some((f) => name(f) === p), `missing page ${p}`);
  }
});

test('every page has title, description, canonical, language and one h1', () => {
  for (const f of pages) {
    const h = read(f), n = name(f);
    assert.ok(/<title>[^<]{3,}<\/title>/.test(h), `${n} title`);
    assert.ok(/<meta name="description" content="[^"]{30,}"/.test(h), `${n} needs a description of at least 30 characters`);
    assert.ok(h.includes(`<link rel="canonical" href="${SITE}${n}"`), `${n} canonical`);
    assert.ok(/<html[^>]*\blang="/.test(h), `${n} lang`);
    assert.equal((h.match(/<h1[\s>]/g) ?? []).length, 1, `${n} must have exactly one h1`);
  }
});

test('no em dashes anywhere in source or output', () => {
  const src = walk(join(root, 'src')).filter((f) => /\.(md|mdx|astro|ts|css)$/.test(f));
  for (const f of src) assert.ok(!read(f).includes('—'), `em dash in ${relative(root, f)}`);
  for (const f of pages) assert.ok(!text(read(f)).includes('—'), `em dash in ${name(f)}`);
});

test('landing copy talks about impact, not tech stack', () => {
  const t = text(home);
  for (const w of ['SQLite', 'Ed25519', 'HMAC', 'Docker', 'Golang', 'htmx', 'Postgres']) {
    assert.ok(!t.includes(w), `landing mentions ${w}`);
  }
  assert.ok(!/Pre-alpha|Built in the open/.test(t), 'removed badge is back');
  assert.ok(!/No account\. No subscription\. AGPL/.test(t), 'removed line is back');
});

test('icons are SVG, never emoji', () => {
  assert.ok(!/\p{Extended_Pictographic}/u.test(text(home)), 'emoji found on landing page');
});

test('images have alt text and dimensions, and are never cropped', () => {
  for (const f of pages) {
    for (const tag of read(f).match(/<img\b[^>]*>/g) ?? []) {
      if (tag.includes('astro-')) continue;
      assert.match(tag, /\balt="[^"]+"/, `${name(f)} image without alt: ${tag.slice(0, 80)}`);
    }
  }
  for (const tag of home.match(/<img\b[^>]*>/g)) assert.match(tag, /\bwidth=/, 'landing image needs width and height');
  const css = read(join(root, 'src/styles/site.css'));
  assert.ok(!/object-fit:\s*cover/.test(css), 'object-fit: cover would crop screenshots');
});

test('internal links and anchors resolve', () => {
  for (const f of pages) {
    const h = read(f), n = name(f);
    for (const [, href] of h.matchAll(/<a\b[^>]*\bhref="([^"]+)"/g)) {
      if (/^(https?:|mailto:|tel:)/.test(href)) continue;
      const [path, hash] = href.split('#');
      if (path) {
        const target = join(dist, path.split('?')[0]);
        const ok = existsSync(target) && (statSync(target).isFile() || existsSync(join(target, 'index.html')));
        assert.ok(ok, `${n} links to missing ${href}`);
      } else if (hash) {
        assert.ok(h.includes(`id="${hash}"`), `${n} anchor #${hash} not found`);
      }
    }
  }
});

test('structured data parses and FAQ markup matches the visible FAQ', () => {
  const blocks = [...home.matchAll(/<script type="application\/ld\+json">([\s\S]*?)<\/script>/g)].map((m) => JSON.parse(m[1]));
  const types = blocks.map((b) => b['@type']);
  for (const t of ['Organization', 'WebSite', 'SoftwareApplication', 'FAQPage']) assert.ok(types.includes(t), `missing ${t}`);
  const faq = blocks.find((b) => b['@type'] === 'FAQPage');
  assert.equal(faq.mainEntity.length, (home.match(/<summary/g) ?? []).length, 'FAQ count differs from markup');
  assert.equal(blocks.find((b) => b['@type'] === 'SoftwareApplication').offers.price, '0');
});

test('robots, sitemap and llms.txt are present and complete', () => {
  assert.match(read(join(dist, 'robots.txt')), new RegExp(`Sitemap: ${SITE}/sitemap-index.xml`));
  assert.ok(existsSync(join(dist, 'sitemap-index.xml')));
  const llms = read(join(dist, 'llms.txt'));
  for (const f of pages.filter((p) => name(p).startsWith('/docs/'))) assert.ok(llms.includes(`${SITE}${name(f)}`), `llms.txt misses ${name(f)}`);
  assert.ok(read(join(dist, 'llms-full.txt')).length > 500);
});

// WCAG contrast for the brand pairs the site relies on
const lum = (hex) => {
  const c = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255).map((v) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
  return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
};
const ratio = (a, b) => { const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p); return (x + 0.05) / (y + 0.05); };
test('brand colour pairs meet WCAG AA (4.5:1)', () => {
  const tokens = Object.fromEntries([...read(join(root, 'src/styles/tokens.css')).matchAll(/--([\w-]+):\s*(#[0-9A-Fa-f]{6})/g)].map((m) => [m[1], m[2]]));
  const pairs = [
    ['ink', 'paper'], ['body', 'paper'], ['grey', 'paper'], ['green-deep', 'paper'], ['green-deep', 'green-tint'],
    ['amber', 'amber-tint'], ['amber', 'paper'], ['red', 'paper'],
  ];
  for (const [fg, bg] of pairs) assert.ok(ratio(tokens[fg], tokens[bg]) >= 4.5, `${fg} on ${bg} = ${ratio(tokens[fg], tokens[bg]).toFixed(2)}`);
  assert.ok(ratio('#FFFFFF', tokens.green) >= 4.5, 'white on green');
  assert.ok(ratio('#E8ECEA', '#101614') >= 4.5, 'terminal text on dark');
  assert.ok(ratio('#D3DBD7', '#101614') >= 4.5, 'dark section body text');
  assert.ok(ratio(tokens.mint, '#101614') >= 4.5, 'mint on dark');
  assert.ok(ratio('#E3F2EC', tokens.green) >= 4.5, 'CTA text on green');
});

test('motion respects reduced-motion preference', () => {
  assert.match(read(join(root, 'src/styles/site.css')), /prefers-reduced-motion/);
  assert.match(read(join(root, 'src/components/Paisu.astro')), /prefers-reduced-motion/);
});

test('legal drafts carry the draft banner', () => {
  for (const f of ['bank-terms.md', 'privacy.md']) {
    const body = readFileSync(join(root, 'src/content/docs/docs/reference/', f), 'utf8');
    assert.ok(body.includes('Draft'), `${f} needs the draft banner`);
  }
});
