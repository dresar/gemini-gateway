'use client';

import { useEffect, useMemo, useState } from 'react';
import { validateKeysPreflight } from '@/lib/api';
import { normalizeClientKey, validateSingleClientKey } from '@/lib/keyValidator';
import type { KeyValidatePreviewResponse, ProviderView } from '@/lib/types';

interface DraftKeyItem {
  key: string;
  label?: string;
}

function splitRawLinesForCheck(raw: string): DraftKeyItem[] {
  const txt = raw.trim();
  if (!txt) return [];
  if (txt.startsWith('[') || txt.startsWith('{')) {
    try {
      const parsed = JSON.parse(txt) as unknown;
      const arr = Array.isArray(parsed)
        ? parsed
        : parsed && typeof parsed === 'object' && Array.isArray((parsed as { keys?: unknown[] }).keys)
          ? (parsed as { keys: unknown[] }).keys
          : null;
      if (arr) {
        const out: DraftKeyItem[] = [];
        for (const el of arr) {
          if (typeof el === 'string') out.push({ key: el });
          else if (el && typeof el === 'object') {
            const obj = el as Record<string, unknown>;
            const k = String(obj.key ?? obj.api_key ?? obj.token ?? '');
            const l = String(obj.label ?? obj.name ?? '');
            if (k.trim()) out.push({ key: k, label: l });
          }
        }
        return out;
      }
    } catch {
      return [{ key: txt }];
    }
  }
  const out: DraftKeyItem[] = [];
  for (const rawLine of txt.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line || line.startsWith('#') || line.startsWith('//')) continue;
    if (line.startsWith('[') && line.endsWith(']')) {
      try {
        const arr = JSON.parse(line) as unknown;
        if (Array.isArray(arr) && arr.length > 0) {
          for (const el of arr) {
            if (typeof el === 'string' && el.trim()) out.push({ key: el.trim() });
            else if (el && typeof el === 'object') {
              const obj = el as Record<string, unknown>;
              const k = String(obj.key ?? obj.api_key ?? obj.token ?? '');
              const l = String(obj.label ?? obj.name ?? '');
              if (k.trim()) out.push({ key: k.trim(), label: l.trim() });
            }
          }
          continue;
        }
      } catch {
      }
    }
    if (line.includes('=') && !line.startsWith('http')) {
      const eqIdx = line.indexOf('=');
      const val = line.slice(eqIdx + 1).trim();
      if (val) out.push({ key: val });
      continue;
    }
    if (line.includes(';') || (line.includes(',') && line.split(',').length > 2)) {
      for (const seg of line.split(/[;,]+/)) {
        const c = seg.trim();
        if (c) out.push({ key: c });
      }
      continue;
    }
    if (line.includes(',')) {
      const parts = line.split(',').map((s) => s.trim());
      if (parts.length === 2 && parts[0] && parts[1]) {
        const aKey = parts[0].length >= 16 || parts[0].includes('AIza') || parts[0].includes('sk-');
        const bKey = parts[1].length >= 16 || parts[1].includes('AIza') || parts[1].includes('sk-');
        if (!aKey && bKey) {
          out.push({ key: parts[1], label: parts[0] });
          continue;
        }
        if (aKey && !bKey) {
          out.push({ key: parts[0], label: parts[1] });
          continue;
        }
      }
    }
    const tokens = line.split(/\s+/).filter(Boolean);
    if (tokens.length === 2 && tokens[0].toLowerCase() === 'bearer') {
      out.push({ key: tokens[1] });
      continue;
    }
    if (tokens.length > 1 && tokens.every((t) => t.length >= 12 || t.startsWith('AIza') || t.startsWith('sk-'))) {
      for (const tok of tokens) out.push({ key: tok });
      continue;
    }
    out.push({ key: line });
  }
  return out;
}

export function KeyValidationPanel({
  mode,
  provider,
  providerObj,
  rawText,
  rows,
  labelPrefix,
  onCleanImportText,
  onCleanFormRows,
}: {
  mode: 'form' | 'import';
  provider: string;
  providerObj: ProviderView | undefined;
  rawText: string;
  rows: Array<{ key: string; label: string }>;
  labelPrefix: string;
  onCleanImportText: (nextText: string) => void;
  onCleanFormRows: (nextRows: Array<{ key: string; label: string }>) => void;
}) {
  const [serverPreview, setServerPreview] = useState<KeyValidatePreviewResponse | null>(null);

  const itemsToCheck = useMemo<DraftKeyItem[]>(() => {
    if (mode === 'form') {
      return rows.filter((r) => r.key.trim().length > 0);
    }
    return splitRawLinesForCheck(rawText);
  }, [mode, rawText, rows]);

  useEffect(() => {
    if (!provider || itemsToCheck.length === 0) {
      setServerPreview(null);
      return;
    }
    let alive = true;
    const timer = window.setTimeout(() => {
      void validateKeysPreflight({
        provider,
        label_prefix: labelPrefix.trim() || undefined,
        raw_text: mode === 'import' ? rawText : undefined,
        items: mode === 'form' ? itemsToCheck : undefined,
      })
        .then((res) => {
          if (alive) setServerPreview(res);
        })
        .catch(() => {
          if (alive) setServerPreview(null);
        });
    }, 260);
    return () => {
      alive = false;
      window.clearTimeout(timer);
    };
  }, [provider, mode, rawText, labelPrefix, itemsToCheck]);

  const clientAnalysis = useMemo(() => {
    const seen = new Set<string>();
    const validList: Array<{ key: string; label: string }> = [];
    const issueReasons: string[] = [];
    let batchDups = 0;
    let invalidCount = 0;

    itemsToCheck.forEach((it, idx) => {
      const check = validateSingleClientKey(providerObj, it.key);
      if (check.error) {
        invalidCount++;
        if (issueReasons.length < 4) issueReasons.push(`#${idx + 1}: ${check.error}`);
        return;
      }
      if (seen.has(check.clean)) {
        batchDups++;
        if (issueReasons.length < 4) issueReasons.push(`#${idx + 1}: Duplikat dalam daftar`);
        return;
      }
      seen.add(check.clean);
      validList.push({ key: check.clean, label: (it.label || '').trim() });
    });

    return { validList, issueReasons, batchDups, invalidCount };
  }, [itemsToCheck, providerObj]);

  if (itemsToCheck.length === 0) return null;

  const totalValid = serverPreview ? serverPreview.valid : clientAnalysis.validList.length;
  const totalDups = serverPreview
    ? serverPreview.pool_duplicates + serverPreview.batch_duplicates
    : clientAnalysis.batchDups;
  const totalInvalid = serverPreview ? serverPreview.invalid : clientAnalysis.invalidCount;
  const totalCross = serverPreview ? serverPreview.cross_duplicates : 0;

  const previewIssues =
    serverPreview?.items
      .filter((it) => it.status !== 'valid' || it.warn_reason)
      .slice(0, 4)
      .map((it) =>
        it.status === 'valid'
          ? `${it.masked}: ${it.warn_reason}`
          : `#${it.index} (${it.masked}): ${it.reason || it.status}`,
      ) || clientAnalysis.issueReasons;

  const handleClean = () => {
    if (serverPreview?.items?.length) {
      const keepIndices = new Set(
        serverPreview.items.filter((it) => it.status === 'valid').map((it) => it.index - 1),
      );
      const cleaned = itemsToCheck
        .filter((_, idx) => keepIndices.has(idx))
        .map((it) => ({ key: normalizeClientKey(it.key), label: (it.label || '').trim() }));
      if (mode === 'form') {
        onCleanFormRows(cleaned.length > 0 ? cleaned : [{ key: '', label: '' }]);
      } else {
        onCleanImportText(cleaned.map((c) => (c.label ? `${c.label}, ${c.key}` : c.key)).join('\n'));
      }
      return;
    }
    const fallback = clientAnalysis.validList;
    if (mode === 'form') {
      onCleanFormRows(fallback.length > 0 ? fallback : [{ key: '', label: '' }]);
    } else {
      onCleanImportText(fallback.map((c) => (c.label ? `${c.label}, ${c.key}` : c.key)).join('\n'));
    }
  };

  return (
    <div className="neu-inset-box" style={{ marginBottom: '0.75rem', fontSize: '0.75rem' }}>
      <div className="pill-row" style={{ justifyContent: 'space-between', alignItems: 'center' }}>
        <div className="pill-row">
          <span className="badge badge-ok">{totalValid} valid</span>
          {totalDups > 0 ? <span className="badge badge-warn">{totalDups} duplikat</span> : null}
          {totalInvalid > 0 ? <span className="badge badge-err">{totalInvalid} invalid</span> : null}
          {totalCross > 0 ? <span className="badge badge-warn">{totalCross} lintas</span> : null}
        </div>
        {totalDups > 0 || totalInvalid > 0 ? (
          <button type="button" className="neu-btn neu-btn-sm" onClick={handleClean}>
            Bersihkan
          </button>
        ) : null}
      </div>
      {previewIssues.length > 0 ? (
        <ul style={{ margin: '0.45rem 0 0 1rem', padding: 0, color: 'var(--text-muted)' }}>
          {previewIssues.map((line, i) => (
            <li key={i} className="mono-wrap">
              {line}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
