import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(__dirname, '..');
const outDir = path.join(webRoot, 'out');
const htmlPath = path.join(outDir, 'index.html');
const targetPath = path.resolve(webRoot, '..', 'cmd', 'ui', 'index.html');

let html = fs.readFileSync(htmlPath, 'utf8');

function escapeInlineScript(js) {
  return js.replace(/<\/script/gi, '<\\/script');
}

function escapeInlineStyle(css) {
  return css.replace(/<\/style/gi, '<\\/style');
}

function readStaticAsset(href) {
  const clean = href.split('?')[0].replace(/^\/+/, '');
  const full = path.join(outDir, clean);
  if (!fs.existsSync(full)) {
    throw new Error(`Missing asset referenced by index.html: ${full}`);
  }
  return fs.readFileSync(full, 'utf8');
}

html = html.replace(/<link\b[^>]*\brel="preload"[^>]*>/gi, '');

html = html.replace(/<link\b[^>]*\brel="stylesheet"[^>]*>/gi, (tag) => {
  const hrefMatch = tag.match(/\bhref="([^"]+)"/i);
  if (!hrefMatch) return tag;
  const css = readStaticAsset(hrefMatch[1]);
  return `<style data-inlined="true">${escapeInlineStyle(css)}</style>`;
});

html = html.replace(/<script\b([^>]*)\bsrc="([^"]+)"([^>]*)><\/script>/gi, (tag, pre, src, post) => {
  const attrs = `${pre} ${post}`;
  if (/\bnoModule\b/i.test(attrs)) {
    return '';
  }
  const js = readStaticAsset(src);
  const idMatch = attrs.match(/\bid="([^"]+)"/i);
  const idAttr = idMatch ? ` id="${idMatch[1]}"` : '';
  return `<script${idAttr}>${escapeInlineScript(js)}</script>`;
});

html = html.replace(/:HL\[\\"\/_next\/static\/css\/[^\\"]+\\",\\"style\\"\]\\n/g, '');
html = html.replace(
  /\[\\"\$\\",\\"link\\",\\"[^"]*\\",\{[^}]*\\"rel\\":\\"stylesheet\\",\\"href\\":\\"\/_next\/static\/css\/[^"]*\\"[^}]*\}\],?/g,
  ''
);

const leftover = html.match(/.{0,60}_next\/static.{0,80}/g);
if (leftover) {
  console.warn('Remaining _next/static references in index.html:', leftover);
}

fs.mkdirSync(path.dirname(targetPath), { recursive: true });
fs.writeFileSync(targetPath, html, 'utf8');

const kb = (Buffer.byteLength(html, 'utf8') / 1024).toFixed(1);
console.log(`Built single-file HTML: ${targetPath} (${kb} KB)`);
