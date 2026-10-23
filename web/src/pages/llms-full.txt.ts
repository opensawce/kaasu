import { getCollection } from 'astro:content';

export async function GET() {
  const docs = (await getCollection('docs')).sort((a, b) => a.id.localeCompare(b.id));
  const body = docs.map((d) => `# ${d.data.title}\n\n${d.body ?? ''}`).join('\n\n---\n\n');
  return new Response(body, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } });
}
