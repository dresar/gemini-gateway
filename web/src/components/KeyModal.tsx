'use client';

import { useEffect, useMemo, useRef, useState } from 'react';
import type { KeyView, ProviderView } from '@/lib/types';
import { CloseIcon, PlusIcon } from './Icons';
import { KeyValidationPanel } from './KeyValidationPanel';
import { ProviderGlyph } from './ProviderIcons';

function installLightDismissFallback(dialog: HTMLDialogElement) {
  if ('closedBy' in HTMLDialogElement.prototype) return () => {};
  const outside = (e: PointerEvent) => {
    const r = dialog.getBoundingClientRect();
    return e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom;
  };
  let pressedOutside = false;
  const onDown = (e: PointerEvent) => {
    pressedOutside = outside(e);
  };
  const onUp = (e: PointerEvent) => {
    if (pressedOutside && outside(e)) {
      dialog.close('dismiss');
    }
  };
  dialog.addEventListener('pointerdown', onDown);
  dialog.addEventListener('pointerup', onUp);
  return () => {
    dialog.removeEventListener('pointerdown', onDown);
    dialog.removeEventListener('pointerup', onUp);
  };
}

interface ParsedKeyItem {
  key: string;
  label: string;
}

function cleanPrefix(rawPrefix: string, providerId: string): string {
  const base = (rawPrefix.trim() || providerId || 'key')
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, '');
  return base || 'key';
}

function nextStartIndex(existingKeys: KeyView[], prefix: string): number {
  let maxNum = 0;
  const lowerPrefix = prefix.toLowerCase();
  for (const k of existingKeys) {
    const lbl = (k.label || '').trim().toLowerCase();
    if (!lbl.startsWith(lowerPrefix)) continue;
    const rest = lbl.slice(lowerPrefix.length).replace(/^[#_\-\s]+/, '');
    if (/^\d+$/.test(rest)) {
      const n = Number.parseInt(rest, 10);
      if (n > maxNum) maxNum = n;
    }
  }
  return maxNum + 1;
}

function formatSeqLabel(prefix: string, seq: number): string {
  return `${prefix}${String(seq).padStart(2, '0')}`;
}

function parseSmartKeysText(raw: string): ParsedKeyItem[] {
  const txt = raw.trim();
  if (!txt) return [];
  const out: ParsedKeyItem[] = [];
  const seen = new Set<string>();
  const pushItem = (keyRaw: string, labelRaw = '') => {
    const k = keyRaw.trim().replace(/^["'`]+|["'`,;]+$/g, '');
    if (!k || k.startsWith('#') || k.length < 4 || seen.has(k)) return;
    seen.add(k);
    out.push({ key: k, label: labelRaw.trim() });
  };

  if (txt.startsWith('[') || txt.startsWith('{')) {
    try {
      const parsed = JSON.parse(txt) as unknown;
      const arr = Array.isArray(parsed)
        ? parsed
        : parsed && typeof parsed === 'object' && Array.isArray((parsed as { keys?: unknown[] }).keys)
          ? (parsed as { keys: unknown[] }).keys
          : null;
      if (arr) {
        for (const el of arr) {
          if (typeof el === 'string') {
            pushItem(el);
          } else if (el && typeof el === 'object') {
            const obj = el as Record<string, unknown>;
            const k = String(obj.key ?? obj.api_key ?? obj.token ?? '');
            const l = String(obj.label ?? obj.name ?? '');
            pushItem(k, l);
          }
        }
        if (out.length > 0) return out;
      }
    } catch {
    }
  }

  const lines = txt.split(/\r?\n/);
  for (const rawLine of lines) {
    let line = rawLine.trim();
    if (!line || line.startsWith('#') || line.startsWith('//')) continue;

    if (line.includes('=') && !line.startsWith('http')) {
      const eqIdx = line.indexOf('=');
      const envName = line.slice(0, eqIdx).trim();
      const envVal = line.slice(eqIdx + 1).trim();
      if (envVal) {
        pushItem(envVal, /^[A-Za-z0-9_]+$/.test(envName) ? '' : envName);
        continue;
      }
    }

    if (line.includes(';') || (line.includes(',') && line.split(',').length > 2)) {
      for (const seg of line.split(/[;,]+/)) {
        const cleaned = seg.trim();
        if (cleaned) pushItem(cleaned);
      }
      continue;
    }

    if (line.includes(',')) {
      const parts = line.split(',').map((s) => s.trim());
      if (parts.length === 2 && parts[0] && parts[1]) {
        const aLooksKey = parts[0].length >= 16 || parts[0].includes('AIza') || parts[0].includes('sk-');
        const bLooksKey = parts[1].length >= 16 || parts[1].includes('AIza') || parts[1].includes('sk-');
        if (!aLooksKey && bLooksKey) {
          pushItem(parts[1], parts[0]);
        } else if (aLooksKey && !bLooksKey) {
          pushItem(parts[0], parts[1]);
        } else {
          pushItem(parts[0]);
          pushItem(parts[1]);
        }
        continue;
      }
    }

    const spaceTokens = line.split(/\s+/).filter(Boolean);
    if (spaceTokens.length > 1 && spaceTokens.every((tok) => tok.length >= 8)) {
      for (const tok of spaceTokens) {
        pushItem(tok);
      }
      continue;
    }

    line = spaceTokens[0] || '';
    pushItem(line);
  }
  return out;
}

export function AddKeysDialog({
  open,
  providers,
  existingKeys,
  defaultProvider,
  busy,
  onClose,
  onSubmit,
}: {
  open: boolean;
  providers: ProviderView[];
  existingKeys: KeyView[];
  defaultProvider: string;
  busy: boolean;
  onClose: () => void;
  onSubmit: (payload: {
    provider: string;
    raw_text?: string;
    label_prefix?: string;
    items?: Array<{ key: string; label?: string }>;
  }) => Promise<void>;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const [mode, setMode] = useState<'form' | 'import'>('form');
  const [provider, setProvider] = useState(defaultProvider);
  const [labelPrefix, setLabelPrefix] = useState('');
  const [rows, setRows] = useState<Array<{ key: string; label: string }>>([{ key: '', label: '' }]);
  const [rawText, setRawText] = useState('');
  const [importedName, setImportedName] = useState('');
  const [error, setError] = useState('');

  useEffect(() => {
    const dlg = ref.current;
    if (!dlg) return;
    if (open && !dlg.open) {
      const valid = providers.some((p) => p.id === defaultProvider)
        ? defaultProvider
        : providers[0]?.id || '';
      setProvider(valid);
      setMode('form');
      setLabelPrefix('');
      setRows([{ key: '', label: '' }]);
      setRawText('');
      setImportedName('');
      setError('');
      dlg.showModal();
    } else if (!open && dlg.open) {
      dlg.close();
    }
  }, [open, defaultProvider, providers]);

  useEffect(() => {
    const dlg = ref.current;
    if (!dlg) return;
    const onCloseEvt = () => {
      dlg.returnValue = '';
      onClose();
    };
    const cleanupLight = installLightDismissFallback(dlg);
    dlg.addEventListener('close', onCloseEvt);
    return () => {
      cleanupLight();
      dlg.removeEventListener('close', onCloseEvt);
    };
  }, [onClose]);

  const effectivePrefix = useMemo(() => cleanPrefix(labelPrefix, provider), [labelPrefix, provider]);
  const providerExistingKeys = useMemo(
    () => existingKeys.filter((k) => k.provider === provider),
    [existingKeys, provider],
  );
  const startSeq = useMemo(
    () => nextStartIndex(providerExistingKeys, effectivePrefix),
    [providerExistingKeys, effectivePrefix],
  );

  const parsedBulkItems = useMemo(() => parseSmartKeysText(rawText), [rawText]);

  const resolvedPreviewItems = useMemo(() => {
    if (mode === 'import') {
      return parsedBulkItems.map((it, idx) => ({
        key: it.key,
        label: it.label || formatSeqLabel(effectivePrefix, startSeq + idx),
      }));
    }
    const clean: ParsedKeyItem[] = [];
    const seen = new Set<string>();
    let seqOffset = 0;
    for (const r of rows) {
      const k = r.key.trim();
      if (!k || seen.has(k)) continue;
      seen.add(k);
      const lbl = r.label.trim() || formatSeqLabel(effectivePrefix, startSeq + seqOffset);
      seqOffset++;
      clean.push({ key: k, label: lbl });
    }
    return clean;
  }, [mode, parsedBulkItems, rows, effectivePrefix, startSeq]);

  const applyAutoSplitIfMulti = (index: number, candidate: string): boolean => {
    if (/[\r\n,;\s]|^\s*[\[{]/.test(candidate)) {
      const multi = parseSmartKeysText(candidate);
      if (multi.length > 1) {
        setRows((prev) => {
          const next = [...prev];
          next.splice(index, 1, ...multi.map((m) => ({ key: m.key, label: m.label })));
          return next;
        });
        return true;
      }
    }
    return false;
  };

  const handleRowKeyChange = (index: number, nextVal: string) => {
    if (error) setError('');
    if (applyAutoSplitIfMulti(index, nextVal)) return;
    setRows((prev) => prev.map((r, i) => (i === index ? { ...r, key: nextVal } : r)));
  };

  const handleRowPaste = (index: number, e: React.ClipboardEvent<HTMLInputElement>) => {
    const pasted = e.clipboardData?.getData('text');
    if (pasted && applyAutoSplitIfMulti(index, pasted)) {
      e.preventDefault();
      if (error) setError('');
    }
  };

  const handleFileChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    try {
      const text = await file.text();
      const parsed = parseSmartKeysText(text);
      if (parsed.length === 0) {
        setError('File kosong atau format key tidak dikenali.');
        return;
      }
      setRawText(text);
      setImportedName(`${file.name} (${parsed.length})`);
      setError('');
      setMode('import');
    } catch {
      setError('Gagal membaca file.');
    } finally {
      e.target.value = '';
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!provider) {
      setError('Tambah provider dulu.');
      return;
    }
    if (resolvedPreviewItems.length === 0) {
      setError('Isi minimal 1 API Key.');
      return;
    }
    setError('');
    await onSubmit({
      provider,
      label_prefix: effectivePrefix,
      items: resolvedPreviewItems,
    });
  };

  return (
    <dialog
      ref={ref}
      id="add-keys-dialog"
      aria-labelledby="add-keys-title"
      {...({ closedby: 'any' } as Record<string, string>)}
    >
      <div className="card-head" style={{ marginBottom: '0.75rem' }}>
        <h1 id="add-keys-title" style={{ margin: 0 }}>
          Tambah Key
        </h1>
        <span className="badge badge-ok">{resolvedPreviewItems.length} siap</span>
      </div>

      <form onSubmit={handleSubmit}>
        <div className="two-col">
          <div className="field">
            <label htmlFor="add-key-provider">Provider</label>
            <select
              id="add-key-provider"
              value={provider}
              onChange={(e) => setProvider(e.target.value)}
            >
              {providers.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="add-label-prefix">Prefix Nama</label>
            <input
              id="add-label-prefix"
              name="label_prefix"
              type="text"
              placeholder={provider || 'gemini'}
              value={labelPrefix}
              onChange={(e) => setLabelPrefix(e.target.value)}
              autoComplete="off"
              autoCorrect="off"
              autoCapitalize="none"
            />
          </div>
        </div>

        <div className="toolbar" style={{ marginBottom: '0.75rem' }}>
          <div className="pill-row">
            <button
              type="button"
              className={`neu-btn neu-btn-sm ${mode === 'form' ? 'is-active' : ''}`}
              onClick={() => setMode('form')}
            >
              Form
            </button>
            <button
              type="button"
              className={`neu-btn neu-btn-sm ${mode === 'import' ? 'is-active' : ''}`}
              onClick={() => setMode('import')}
            >
              Tempel / Import
            </button>
          </div>
          <input
            ref={fileInputRef}
            type="file"
            accept=".txt,.csv,.json,.env,.pool,text/*"
            style={{ display: 'none' }}
            onChange={(e) => void handleFileChange(e)}
          />
          <button
            type="button"
            className="neu-btn neu-btn-sm"
            onClick={() => fileInputRef.current?.click()}
          >
            Import File
          </button>
        </div>

        {mode === 'form' ? (
          <div className="field">
            <div className="card-head" style={{ marginBottom: '0.45rem' }}>
              <label style={{ margin: 0 }}>
                Baris Key ({formatSeqLabel(effectivePrefix, startSeq)}…)
              </label>
              <button
                type="button"
                className="neu-btn neu-btn-sm"
                onClick={() => setRows((prev) => [...prev, { key: '', label: '' }])}
              >
                <PlusIcon size={14} />
                <span>Baris</span>
              </button>
            </div>
            <div className="key-rows-list">
              {rows.map((r, idx) => {
                const autoLbl = formatSeqLabel(effectivePrefix, startSeq + idx);
                return (
                  <div key={idx} className="key-form-row">
                    <input
                      type="text"
                      aria-label={`Label ${idx + 1}`}
                      placeholder={autoLbl}
                      value={r.label}
                      onChange={(e) =>
                        setRows((prev) =>
                          prev.map((item, i) => (i === idx ? { ...item, label: e.target.value } : item)),
                        )
                      }
                      className="key-row-label"
                      autoCorrect="off"
                      autoCapitalize="none"
                      spellCheck={false}
                    />
                    <input
                      type="text"
                      aria-label={`API Key ${idx + 1}`}
                      placeholder="Tempel 1 atau banyak key"
                      value={r.key}
                      autoFocus={idx === 0}
                      onChange={(e) => handleRowKeyChange(idx, e.target.value)}
                      onPaste={(e) => handleRowPaste(idx, e)}
                      autoCorrect="off"
                      autoCapitalize="none"
                      spellCheck={false}
                    />
                    {rows.length > 1 ? (
                      <button
                        type="button"
                        className="neu-icon-btn"
                        aria-label={`Hapus baris ${idx + 1}`}
                        onClick={() => setRows((prev) => prev.filter((_, i) => i !== idx))}
                      >
                        <CloseIcon size={15} />
                      </button>
                    ) : null}
                  </div>
                );
              })}
            </div>
          </div>
        ) : (
          <div className="field">
            <label htmlFor="add-keys-textarea">
              {importedName ? `File: ${importedName}` : `Tempel / CSV / JSON (${parsedBulkItems.length})`}
            </label>
            <textarea
              id="add-keys-textarea"
              name="keys"
              rows={5}
              autoFocus
              placeholder="Tempel banyak key (baris, koma, JSON, .env)"
              value={rawText}
              onChange={(e) => {
                setRawText(e.target.value);
                if (error) setError('');
              }}
              aria-invalid={error ? 'true' : undefined}
              autoCorrect="off"
              autoCapitalize="none"
              spellCheck={false}
            />
          </div>
        )}

        {resolvedPreviewItems.length > 0 ? (
          <div className="neu-inset-box" style={{ marginBottom: '0.75rem', fontSize: '0.75rem' }}>
            <span className="kpi-sub">
              Urutan nama:{' '}
              <strong>
                {resolvedPreviewItems
                  .slice(0, 4)
                  .map((i) => i.label)
                  .join(', ')}
                {resolvedPreviewItems.length > 4
                  ? ` … ${resolvedPreviewItems[resolvedPreviewItems.length - 1].label}`
                  : ''}
              </strong>
            </span>
          </div>
        ) : null}

        <KeyValidationPanel
          mode={mode}
          provider={provider}
          providerObj={providers.find((p) => p.id === provider)}
          rawText={rawText}
          rows={rows}
          labelPrefix={labelPrefix}
          onCleanImportText={(nextText) => setRawText(nextText)}
          onCleanFormRows={(nextRows) => setRows(nextRows)}
        />

        {error ? (
          <span className="field-err" role="alert">
            {error}
          </span>
        ) : null}

        <div className="dialog-actions">
          <button type="button" className="neu-btn" onClick={() => ref.current?.close('cancel')}>
            Batal
          </button>
          <button type="submit" className="neu-btn-primary" disabled={busy}>
            {busy ? 'Proses...' : 'Simpan'}
          </button>
        </div>
      </form>
    </dialog>
  );
}

export function CreateClientKeyDialog({
  open,
  providers,
  busy,
  onClose,
  onSubmit,
}: {
  open: boolean;
  providers: ProviderView[];
  busy: boolean;
  onClose: () => void;
  onSubmit: (payload: { name: string; scope: string; custom_key?: string }) => Promise<void>;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const [name, setName] = useState('');
  const [scope, setScope] = useState('global');
  const [customKey, setCustomKey] = useState('');
  const [error, setError] = useState('');

  const categories = useMemo(() => {
    const set = new Set<string>();
    for (const p of providers) {
      if (p.category) set.add(p.category);
    }
    return Array.from(set);
  }, [providers]);

  useEffect(() => {
    const dlg = ref.current;
    if (!dlg) return;
    if (open && !dlg.open) {
      setName('');
      setScope('global');
      setCustomKey('');
      setError('');
      dlg.showModal();
    } else if (!open && dlg.open) {
      dlg.close();
    }
  }, [open]);

  useEffect(() => {
    const dlg = ref.current;
    if (!dlg) return;
    const onCloseEvt = () => {
      dlg.returnValue = '';
      onClose();
    };
    const cleanupLight = installLightDismissFallback(dlg);
    dlg.addEventListener('close', onCloseEvt);
    return () => {
      cleanupLight();
      dlg.removeEventListener('close', onCloseEvt);
    };
  }, [onClose]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) {
      setError('Nama wajib diisi.');
      return;
    }
    if (customKey.trim() && customKey.trim().length < 8) {
      setError('Minimal 8 karakter.');
      return;
    }
    setError('');
    await onSubmit({
      name: name.trim(),
      scope,
      custom_key: customKey.trim() || undefined,
    });
  };

  return (
    <dialog
      ref={ref}
      id="create-client-key-dialog"
      aria-labelledby="create-client-key-title"
      {...({ closedby: 'any' } as Record<string, string>)}
    >
      <h1 id="create-client-key-title">Buat Key</h1>
      <form onSubmit={handleSubmit}>
        <div className="field">
          <label htmlFor="ck-name">Nama</label>
          <input
            id="ck-name"
            type="text"
            autoFocus
            required
            placeholder="Contoh: Coding Agent"
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              if (error) setError('');
            }}
          />
        </div>

        <div className="field">
          <label>Kategori</label>
          <div className="scope-pick-grid">
            <label className={`seg-option ${scope === 'global' ? 'is-selected' : ''}`}>
              <input
                type="radio"
                name="client_scope"
                value="global"
                checked={scope === 'global'}
                onChange={() => setScope('global')}
              />
              <ProviderGlyph icon="globe" size={16} />
              <span>Global</span>
            </label>
            {categories.map((cat) => {
              const val = `cat:${cat}`;
              return (
                <label
                  key={val}
                  className={`seg-option ${scope === val ? 'is-selected' : ''}`}
                >
                  <input
                    type="radio"
                    name="client_scope"
                    value={val}
                    checked={scope === val}
                    onChange={() => setScope(val)}
                  />
                  <ProviderGlyph icon="custom" size={16} />
                  <span>Grup: {cat}</span>
                </label>
              );
            })}
            {providers.map((pr) => (
              <label
                key={pr.id}
                className={`seg-option ${scope === pr.id ? 'is-selected' : ''}`}
              >
                <input
                  type="radio"
                  name="client_scope"
                  value={pr.id}
                  checked={scope === pr.id}
                  onChange={() => setScope(pr.id)}
                />
                <ProviderGlyph icon={pr.icon} size={16} />
                <span>{pr.name}</span>
              </label>
            ))}
          </div>
        </div>

        <div className="field">
          <label htmlFor="ck-custom">Key Kustom</label>
          <input
            id="ck-custom"
            type="text"
            placeholder="Opsional (otomatis)"
            value={customKey}
            onChange={(e) => {
              setCustomKey(e.target.value);
              if (error) setError('');
            }}
            autoCorrect="off"
            autoCapitalize="none"
            spellCheck={false}
          />
        </div>

        {error ? (
          <p className="field-err" role="alert">
            {error}
          </p>
        ) : null}

        <div className="dialog-actions">
          <button type="button" className="neu-btn" onClick={() => ref.current?.close('cancel')}>
            Batal
          </button>
          <button type="submit" className="neu-btn-primary" disabled={busy}>
            {busy ? 'Proses...' : 'Buat'}
          </button>
        </div>
      </form>
    </dialog>
  );
}

export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel,
  busy,
  onCancel,
  onConfirm,
}: {
  open: boolean;
  title: string;
  message: string;
  confirmLabel: string;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => Promise<void>;
}) {
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const dlg = ref.current;
    if (!dlg) return;
    if (open && !dlg.open) {
      dlg.showModal();
    } else if (!open && dlg.open) {
      dlg.close();
    }
  }, [open]);

  useEffect(() => {
    const dlg = ref.current;
    if (!dlg) return;
    const onCloseEvt = () => {
      dlg.returnValue = '';
      onCancel();
    };
    const cleanupLight = installLightDismissFallback(dlg);
    dlg.addEventListener('close', onCloseEvt);
    return () => {
      cleanupLight();
      dlg.removeEventListener('close', onCloseEvt);
    };
  }, [onCancel]);

  return (
    <dialog
      ref={ref}
      id="confirm-action-dialog"
      aria-labelledby="confirm-action-title"
      {...({ closedby: 'any' } as Record<string, string>)}
    >
      <h1 id="confirm-action-title">{title}</h1>
      <p className="dialog-msg">{message}</p>
      <div className="dialog-actions">
        <button
          type="button"
          className="neu-btn"
          autoFocus
          onClick={() => ref.current?.close('cancel')}
        >
          Batal
        </button>
        <button type="button" className="neu-btn-danger" disabled={busy} onClick={onConfirm}>
          {busy ? 'Proses...' : confirmLabel}
        </button>
      </div>
    </dialog>
  );
}
