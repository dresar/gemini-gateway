'use client';

import { useEffect, useState } from 'react';
import { probeProviderModels, testRawKey } from '@/lib/api';
import type { APIType, AuthMode, KeyTestResult, ProviderInput, ProviderView } from '@/lib/types';
import { AlertIcon, CheckCircleIcon, EyeIcon } from './Icons';
import { PROVIDER_ICON_OPTIONS, ProviderGlyph } from './ProviderIcons';

const DEFAULT_CATEGORIES = ['gratis', 'coding', 'cepat', 'multimodal', 'custom'];

export function ProviderFormPage({
  editing,
  busy,
  existingCategories,
  onCancel,
  onSubmit,
}: {
  editing: ProviderView | null;
  busy: boolean;
  existingCategories: string[];
  onCancel: () => void;
  onSubmit: (payload: ProviderInput, initialKeysRaw: string, editingId?: string) => Promise<void>;
}) {
  const [name, setName] = useState('');
  const [category, setCategory] = useState('gratis');
  const [apiType, setApiType] = useState<APIType>('openai');
  const [authMode, setAuthMode] = useState<AuthMode>('bearer');
  const [baseURL, setBaseURL] = useState('');
  const [defaultModel, setDefaultModel] = useState('');
  const [icon, setIcon] = useState('custom');
  const [customIconURL, setCustomIconURL] = useState('');
  const [weight, setWeight] = useState(50);
  const [priority, setPriority] = useState(3);
  const [enabled, setEnabled] = useState(true);
  const [modelRotationEnabled, setModelRotationEnabled] = useState(false);
  const [rotationModelsRaw, setRotationModelsRaw] = useState('');
  const [probingModels, setProbingModels] = useState(false);
  const [probeMsg, setProbeMsg] = useState('');
  const [initialKeysRaw, setInitialKeysRaw] = useState('');
  const [showTestKey, setShowTestKey] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<KeyTestResult | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    if (editing) {
      setName(editing.name);
      setCategory(editing.category || 'gratis');
      setApiType(editing.api_type);
      setAuthMode(editing.auth_mode);
      setBaseURL(editing.base_url);
      setDefaultModel(editing.default_model);
      const isPreset = PROVIDER_ICON_OPTIONS.some((o) => o.id === editing.icon);
      setIcon(isPreset ? editing.icon : 'custom');
      setCustomIconURL(isPreset ? '' : editing.icon);
      setWeight(editing.weight);
      setPriority(editing.priority);
      setEnabled(editing.enabled);
      setModelRotationEnabled(Boolean(editing.model_rotation_enabled));
      setRotationModelsRaw((editing.rotation_models || []).join('\n'));
      setProbeMsg('');
      setInitialKeysRaw('');
    } else {
      setName('');
      setCategory('gratis');
      setApiType('openai');
      setAuthMode('bearer');
      setBaseURL('');
      setDefaultModel('');
      setIcon('custom');
      setCustomIconURL('');
      setWeight(50);
      setPriority(3);
      setEnabled(true);
      setModelRotationEnabled(false);
      setRotationModelsRaw('');
      setProbeMsg('');
      setInitialKeysRaw('');
    }
    setTestResult(null);
    setError('');
  }, [editing]);

  const catOptions = Array.from(new Set([...DEFAULT_CATEGORIES, ...existingCategories])).filter(Boolean);

  const firstKeyLine =
    initialKeysRaw
      .split('\n')
      .map((l) => l.trim())
      .find((l) => l && !l.startsWith('#')) || '';

  const parsedKeysCount = initialKeysRaw
    .split('\n')
    .map((l) => l.trim())
    .filter((l) => l && !l.startsWith('#')).length;

  const handleTestEndpoint = async () => {
    if (!baseURL.trim() || !defaultModel.trim() || !firstKeyLine) {
      setError('Isi URL, model, dan key uji.');
      return;
    }
    setError('');
    setTesting(true);
    try {
      const res = await testRawKey({
        api_key: firstKeyLine,
        base_url: baseURL.trim(),
        api_type: apiType,
        auth_mode: authMode,
        model: defaultModel.trim(),
      });
      setTestResult(res);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Uji gagal');
    } finally {
      setTesting(false);
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim() || !baseURL.trim() || !defaultModel.trim()) {
      setError('Nama, URL, dan model wajib diisi.');
      return;
    }
    setError('');
    const parsedRotation = rotationModelsRaw
      .split(/[\n,]+/)
      .map((s) => s.trim())
      .filter(Boolean);
    await onSubmit(
      {
        name: name.trim(),
        category: category.trim() || 'gratis',
        api_type: apiType,
        auth_mode: authMode,
        base_url: baseURL.trim(),
        default_model: defaultModel.trim(),
        icon: customIconURL.trim() || icon,
        enabled,
        weight: Number(weight) || 50,
        priority: Number(priority) || 1,
        model_rotation_enabled: modelRotationEnabled,
        rotation_models: parsedRotation,
      },
      initialKeysRaw,
      editing?.id,
    );
  };

  const handleQuickProbe = async () => {
    if (!editing?.id) return;
    setError('');
    setProbeMsg('');
    setProbingModels(true);
    try {
      const res = await probeProviderModels(editing.id, { apply: false });
      setRotationModelsRaw((res.working_models || []).join('\n'));
      if ((res.working_models || []).length > 0) {
        setModelRotationEnabled(true);
      }
      setProbeMsg(`${res.working_count}/${res.tested_count} model aktif (${res.duration_ms}ms)`);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Cek model gagal');
    } finally {
      setProbingModels(false);
    }
  };

  return (
    <div className="list-stack">
      <section className="neu-card">
        <div className="card-head" style={{ marginBottom: 0 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.65rem' }}>
            <button type="button" className="neu-btn neu-btn-sm" onClick={onCancel}>
              Kembali
            </button>
            <h1 className="card-title" style={{ fontSize: '1.05rem' }}>
              {editing ? 'Edit Provider' : 'Tambah Provider'}
            </h1>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.45rem' }}>
            <ProviderGlyph icon={customIconURL.trim() || icon} size={22} />
            <span className={enabled ? 'badge badge-ok' : 'badge badge-muted'}>
              {enabled ? 'Aktif' : 'Jeda'}
            </span>
          </div>
        </div>
      </section>

      <form onSubmit={(e) => void handleSubmit(e)} className="list-stack">
        <div className="settings-grid">
          <section className="neu-card">
            <div className="card-head">
              <h2 className="card-title">Identitas</h2>
            </div>
            <div className="field">
              <label htmlFor="pv-page-name">Nama</label>
              <input
                id="pv-page-name"
                type="text"
                autoFocus
                required
                placeholder="Groq / GLM / Qwen"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>

            <div className="field">
              <label htmlFor="pv-page-cat">Kategori</label>
              <div className="pill-row" style={{ marginBottom: '0.45rem' }}>
                {catOptions.map((c) => (
                  <button
                    key={c}
                    type="button"
                    className={`neu-btn neu-btn-sm ${category === c ? 'is-active' : ''}`}
                    onClick={() => setCategory(c)}
                  >
                    {c}
                  </button>
                ))}
              </div>
              <input
                id="pv-page-cat"
                type="text"
                placeholder="gratis / coding / custom"
                value={category}
                onChange={(e) => setCategory(e.target.value)}
                autoCorrect="off"
                autoCapitalize="none"
                spellCheck={false}
              />
            </div>

            <div className="two-col">
              <div className="field">
                <label htmlFor="pv-page-weight">Bobot</label>
                <input
                  id="pv-page-weight"
                  type="number"
                  min={1}
                  max={100}
                  value={weight}
                  onChange={(e) => setWeight(Number(e.target.value) || 50)}
                />
              </div>
              <div className="field">
                <label htmlFor="pv-page-prio">Prioritas</label>
                <input
                  id="pv-page-prio"
                  type="number"
                  min={1}
                  max={999}
                  value={priority}
                  onChange={(e) => setPriority(Number(e.target.value) || 1)}
                />
              </div>
            </div>

            <label className="switch-row">
              <span>Aktif</span>
              <input
                type="checkbox"
                checked={enabled}
                onChange={(e) => setEnabled(e.target.checked)}
              />
            </label>
          </section>

          <section className="neu-card">
            <div className="card-head">
              <h2 className="card-title">Endpoint</h2>
            </div>
            <div className="two-col">
              <div className="field">
                <label htmlFor="pv-page-type">Tipe API</label>
                <select
                  id="pv-page-type"
                  value={apiType}
                  onChange={(e) => {
                    const next = e.target.value as APIType;
                    setApiType(next);
                    if (!editing) {
                      setAuthMode(next === 'gemini' ? 'query_key' : 'bearer');
                    }
                  }}
                >
                  <option value="openai">OpenAI-Compat</option>
                  <option value="gemini">Gemini</option>
                </select>
              </div>
              <div className="field">
                <label htmlFor="pv-page-auth">Auth</label>
                <select
                  id="pv-page-auth"
                  value={authMode}
                  onChange={(e) => setAuthMode(e.target.value as AuthMode)}
                >
                  <option value="bearer">Bearer</option>
                  <option value="x-api-key">x-api-key</option>
                  <option value="query_key">Query Key</option>
                </select>
              </div>
            </div>

            <div className="field">
              <label htmlFor="pv-page-url">Endpoint URL</label>
              <input
                id="pv-page-url"
                type="url"
                required
                placeholder="https://api.groq.com/openai/v1"
                value={baseURL}
                onChange={(e) => setBaseURL(e.target.value)}
                autoCorrect="off"
                autoCapitalize="none"
                spellCheck={false}
              />
            </div>

            <div className="field">
              <label htmlFor="pv-page-model">Model Default</label>
              <input
                id="pv-page-model"
                type="text"
                required
                placeholder="llama-3.3-70b-versatile"
                value={defaultModel}
                onChange={(e) => setDefaultModel(e.target.value)}
                autoCorrect="off"
                autoCapitalize="none"
                spellCheck={false}
              />
            </div>

            <label className="switch-row">
              <span>Aktifkan Rotasi Model</span>
              <input
                type="checkbox"
                checked={modelRotationEnabled}
                onChange={(e) => setModelRotationEnabled(e.target.checked)}
              />
            </label>

            <div className="field">
              <div className="item-top" style={{ marginBottom: '0.35rem' }}>
                <label htmlFor="pv-page-rotmodels" style={{ margin: 0 }}>
                  Daftar Model Rotasi
                </label>
                {editing?.id ? (
                  <button
                    type="button"
                    className="neu-btn neu-btn-sm"
                    disabled={probingModels || busy}
                    onClick={() => void handleQuickProbe()}
                  >
                    {probingModels ? 'Mengecek...' : 'Cek Cepat Model'}
                  </button>
                ) : null}
              </div>
              <textarea
                id="pv-page-rotmodels"
                rows={3}
                placeholder="1 model per baris atau pisah koma"
                value={rotationModelsRaw}
                onChange={(e) => setRotationModelsRaw(e.target.value)}
                autoCorrect="off"
                autoCapitalize="none"
                spellCheck={false}
              />
              {probeMsg ? (
                <p className="kpi-sub" style={{ margin: '0.35rem 0 0' }}>
                  {probeMsg}
                </p>
              ) : null}
            </div>

            <div className="field">
              <label htmlFor="pv-page-keys">
                {editing ? 'Tambah Key Baru' : `API Key Awal (${parsedKeysCount})`}
              </label>
              <div className="pw-wrap" style={{ alignItems: 'flex-start' }}>
                <textarea
                  id="pv-page-keys"
                  rows={3}
                  placeholder="Opsional (1 key per baris)"
                  value={initialKeysRaw}
                  onChange={(e) => setInitialKeysRaw(e.target.value)}
                  style={{
                    WebkitTextSecurity: showTestKey ? 'none' : undefined,
                    paddingInlineEnd: '3.25rem',
                  } as React.CSSProperties}
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                />
                <button
                  type="button"
                  className="pw-eye"
                  style={{ top: '0.35rem' }}
                  onClick={() => setShowTestKey((v) => !v)}
                  aria-label="Tampil key"
                >
                  <EyeIcon off={showTestKey} size={17} />
                </button>
              </div>
            </div>

            <div className="item-actions">
              <button
                type="button"
                className="neu-btn neu-btn-sm"
                disabled={testing || !firstKeyLine}
                onClick={() => void handleTestEndpoint()}
              >
                {testing ? 'Menguji...' : 'Tes Koneksi'}
              </button>
            </div>

            {testResult ? (
              <div className="neu-inset-box" style={{ marginTop: '0.75rem', fontSize: '0.8125rem' }}>
                <div className="item-top" style={{ marginBottom: '0.35rem' }}>
                  <span className={testResult.ok ? 'badge badge-ok' : 'badge badge-err'}>
                    {testResult.ok ? <CheckCircleIcon size={13} /> : <AlertIcon size={13} />}
                    <span>{testResult.ok ? `OK · ${testResult.latency_ms} ms` : `Gagal · ${testResult.status}`}</span>
                  </span>
                  <span className="mono">{testResult.model}</span>
                </div>
                <div className="mono-wrap">
                  {testResult.ok ? testResult.reply || 'OK' : testResult.error || 'Gagal'}
                </div>
              </div>
            ) : null}
          </section>
        </div>

        <section className="neu-card">
          <div className="card-head">
            <h2 className="card-title">Ikon</h2>
          </div>
          <div className="icon-picker-grid" style={{ marginBottom: '0.875rem' }}>
            {PROVIDER_ICON_OPTIONS.map((opt) => (
              <button
                key={opt.id}
                type="button"
                className={`icon-pick-btn ${!customIconURL && icon === opt.id ? 'is-selected' : ''}`}
                onClick={() => {
                  setIcon(opt.id);
                  setCustomIconURL('');
                }}
                aria-label={opt.label}
                title={opt.label}
              >
                <ProviderGlyph icon={opt.id} size={18} />
                <span className="icon-pick-label">{opt.label}</span>
              </button>
            ))}
          </div>

          <div className="field" style={{ marginBottom: 0 }}>
            <label htmlFor="pv-page-icon-url">URL Ikon (Opsional)</label>
            <input
              id="pv-page-icon-url"
              type="url"
              placeholder="https://.../icon.svg"
              value={customIconURL}
              onChange={(e) => setCustomIconURL(e.target.value)}
              autoCorrect="off"
              autoCapitalize="none"
              spellCheck={false}
            />
          </div>
        </section>

        {error ? (
          <div className="neu-card" style={{ padding: '0.75rem 1rem' }}>
            <span className="field-err" role="alert">
              {error}
            </span>
          </div>
        ) : null}

        <section className="neu-card">
          <div className="item-actions" style={{ justifyContent: 'flex-end' }}>
            <button type="button" className="neu-btn" onClick={onCancel}>
              Batal
            </button>
            <button type="submit" className="neu-btn-primary" disabled={busy}>
              {busy ? 'Proses...' : 'Simpan'}
            </button>
          </div>
        </section>
      </form>
    </div>
  );
}
