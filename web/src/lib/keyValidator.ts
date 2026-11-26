import type { KeyView, ProviderView } from './types';

export type ClientKeyCheckStatus = 'valid' | 'batch_duplicate' | 'pool_duplicate' | 'invalid';

export interface ClientKeyCheck {
  index: number;
  rawKey: string;
  cleanKey: string;
  label: string;
  masked: string;
  status: ClientKeyCheckStatus;
  reason?: string;
  warnReason?: string;
}

const PLACEHOLDER_PATTERNS = [
  'your_api_key',
  'your-api-key',
  'masukkan_',
  'api_key_here',
  'replace_me',
  'changeme',
  'placeholder',
  'example_key',
  'sample_key',
  'sk-xxx',
  'sk-your',
  'aiza_your',
  '<api_key>',
  '<your_',
];

const PLACEHOLDER_EXACT = new Set([
  'undefined',
  'null',
  'none',
  'bearer',
  'apikey',
  'api_key',
  'test',
  'demo',
]);

export function normalizeClientKey(raw: string): string {
  let s = raw
    .replace(/[\u200B\u200C\u200D\uFEFF]/g, '')
    .replace(/\u00A0/g, ' ')
    .trim()
    .replace(/^["'`,;]+|["'`,;]+$/g, '')
    .trim();
  if (s.length > 7 && s.slice(0, 7).toLowerCase() === 'bearer ') {
    s = s.slice(7).trim();
  }
  return s;
}

export function maskClientKey(k: string): string {
  const s = k.trim();
  if (s.length <= 8) return `${s.slice(0, 2)}••••`;
  return `${s.slice(0, 6)}•••${s.slice(-4)}`;
}

function isPlaceholder(clean: string): boolean {
  const lower = clean.toLowerCase();
  if (PLACEHOLDER_EXACT.has(lower)) return true;
  if (PLACEHOLDER_PATTERNS.some((p) => lower.includes(p))) return true;
  const stripped = lower.replace(/^sk-/, '').replace(/^aiza/, '').replace(/[-_]/g, '');
  if (stripped.length >= 6 && new Set(stripped).size <= 2) return true;
  return false;
}

export function validateSingleClientKey(
  provider: ProviderView | undefined,
  raw: string,
): { clean: string; error?: string; warning?: string } {
  const clean = normalizeClientKey(raw);
  if (!clean) return { clean: '', error: 'Key kosong' };
  if (/[\s\r\n\t]/.test(clean)) {
    return { clean, error: 'Mengandung spasi atau tab di tengah key' };
  }
  for (let i = 0; i < clean.length; i++) {
    const c = clean.charCodeAt(i);
    if (c < 0x21 || c > 0x7e) {
      return { clean, error: 'Karakter kontrol / non-ASCII terdeteksi' };
    }
  }
  const lower = clean.toLowerCase();
  if (lower.startsWith('http://') || lower.startsWith('https://') || lower.includes('://')) {
    return { clean, error: 'Ini URL endpoint, bukan API key' };
  }
  if (/[{}\[\]]/.test(clean) || clean.includes('":')) {
    return { clean, error: 'Potongan JSON tidak valid' };
  }
  if (isPlaceholder(clean)) {
    return { clean, error: 'Placeholder / key contoh terdeteksi' };
  }
  if (clean.length < 8) {
    return { clean, error: 'Terlalu pendek (< 8 karakter)' };
  }
  if (clean.length > 512) {
    return { clean, error: 'Terlalu panjang (> 512 karakter)' };
  }
  if (provider?.api_type === 'gemini') {
    if (clean.startsWith('sk-')) {
      return { clean, error: 'Format sk- milik OpenAI/Agnes, bukan Gemini' };
    }
    if (clean.startsWith('AIza') && clean.length < 14) {
      return { clean, error: 'Key AIza terpotong (< 14 karakter)' };
    }
    if (clean.startsWith('AIza') && clean.length < 35) {
      return { clean, warning: 'Panjang AIza kurang dari standar (39 karakter)' };
    }
  } else if (provider) {
    if (provider.id === 'agnes' && clean.startsWith('sk-') && clean.length < 20) {
      return { clean, error: 'Key Agnes sk- terpotong (< 20 karakter)' };
    }
    if (clean.startsWith('AIza') && !(provider.base_url || '').toLowerCase().includes('googleapis.com')) {
      return { clean, warning: 'Key AIza (Gemini) pada endpoint OpenAI-compat' };
    }
    if (provider.id === 'groq' && !clean.startsWith('gsk_')) {
      return { clean, warning: 'Key Groq umumnya diawali gsk_' };
    }
    if (provider.id === 'openrouter' && !clean.startsWith('sk-or-')) {
      return { clean, warning: 'Key OpenRouter umumnya diawali sk-or-' };
    }
  }
  return { clean };
}

export function validateBatchClientSide(
  provider: ProviderView | undefined,
  items: Array<{ key: string; label?: string }>,
  existingMaskToServerStatus?: Map<string, { status: string; reason?: string; warnReason?: string }>,
): {
  checks: ClientKeyCheck[];
  validCount: number;
  invalidCount: number;
  batchDuplicateCount: number;
  poolDuplicateCount: number;
  warningCount: number;
} {
  const seen = new Set<string>();
  const checks: ClientKeyCheck[] = [];
  let validCount = 0;
  let invalidCount = 0;
  let batchDuplicateCount = 0;
  let poolDuplicateCount = 0;
  let warningCount = 0;

  items.forEach((item, idx) => {
    const raw = (item.key || '').trim();
    if (!raw || raw.startsWith('#') || raw.startsWith('//')) return;
    const { clean, error, warning } = validateSingleClientKey(provider, raw);
    const masked = maskClientKey(clean || raw);

    if (error) {
      invalidCount++;
      checks.push({
        index: idx + 1,
        rawKey: raw,
        cleanKey: clean,
        label: (item.label || '').trim(),
        masked,
        status: 'invalid',
        reason: error,
      });
      return;
    }

    if (seen.has(clean)) {
      batchDuplicateCount++;
      checks.push({
        index: idx + 1,
        rawKey: raw,
        cleanKey: clean,
        label: (item.label || '').trim(),
        masked,
        status: 'batch_duplicate',
        reason: 'Duplikat dalam daftar input',
      });
      return;
    }
    seen.add(clean);

    const serverHit = existingMaskToServerStatus?.get(String(checks.length + 1));
    if (serverHit?.status === 'pool_duplicate') {
      poolDuplicateCount++;
      checks.push({
        index: idx + 1,
        rawKey: raw,
        cleanKey: clean,
        label: (item.label || '').trim(),
        masked,
        status: 'pool_duplicate',
        reason: serverHit.reason || 'Sudah terdaftar di pool',
      });
      return;
    }

    const warnReason = serverHit?.warnReason || warning;
    if (warnReason) warningCount++;
    validCount++;
    checks.push({
      index: idx + 1,
      rawKey: raw,
      cleanKey: clean,
      label: (item.label || '').trim(),
      masked,
      status: 'valid',
      warnReason,
    });
  });

  return {
    checks,
    validCount,
    invalidCount,
    batchDuplicateCount,
    poolDuplicateCount,
    warningCount,
  };
}

export function cleanExistingLabelMap(keys: KeyView[], providerId: string): number {
  return keys.filter((k) => k.provider === providerId).length;
}
