import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(__dirname, '..');
const iconsDir = path.join(webRoot, 'public', 'assets', 'icons');
const outFile = path.join(webRoot, 'src', 'lib', 'iconAssets.ts');

const mimeByExt = {
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.webp': 'image/webp',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
};

const entries = [];
const files = fs.readdirSync(iconsDir).sort();
for (const file of files) {
  const ext = path.extname(file).toLowerCase();
  const mime = mimeByExt[ext];
  if (!mime) continue;
  const id = path.basename(file, ext);
  const raw = fs.readFileSync(path.join(iconsDir, file));
  const uri = `data:${mime};base64,${raw.toString('base64')}`;
  entries.push([id, uri]);
}

const lines = [
  'export const ICON_DATA_URIS: Record<string, string> = {',
  ...entries.map(([id, uri]) => `  ${JSON.stringify(id)}: ${JSON.stringify(uri)},`),
  '};',
  '',
];

fs.mkdirSync(path.dirname(outFile), { recursive: true });
fs.writeFileSync(outFile, lines.join('\n'), 'utf8');
console.log(`Generated ${entries.length} bundled icon assets -> ${outFile}`);
