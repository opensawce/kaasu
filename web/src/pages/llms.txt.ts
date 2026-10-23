import { getCollection } from 'astro:content';
import { faq } from '../data/faq';

// Machine-readable summary for AI assistants (llmstxt.org). Built from the docs, so it never goes stale.
export async function GET({ site }: { site?: URL }) {
  const docs = (await getCollection('docs')).sort((a, b) => a.id.localeCompare(b.id));
  const link = (id: string) => new URL(id === 'docs' ? 'docs/' : `${id}/`, site).toString();
  const body = `# Kaasu

> Kaasu is free, open source software that confirms UPI payments for small shops in India. Customers pay the merchant's bank directly; Kaasu never holds or moves money. It marks an order paid only when a verified bank alert matches it, and asks the owner when unsure. Licence: AGPL-3.0, with a commercial licence available. Built by OpenSawce, Bengaluru.

## Docs
${docs.map((d) => `- [${d.data.title}](${link(d.id)}): ${d.data.description ?? ''}`).join('\n')}

## Key facts
${faq.map(([q, a]) => `- ${q} ${a}`).join('\n')}

## Project
- [Source code](https://github.com/opensawce/kaasu)
- [Terms](${new URL('terms/', site)})
- Full text of the docs: ${new URL('llms-full.txt', site)}
`;
  return new Response(body, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } });
}
