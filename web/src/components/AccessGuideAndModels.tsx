'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { fetchModelsCatalog } from '@/lib/api';
import type {
  ClientKeyView,
  ModelCatalogItem,
  ModelProviderSummary,
  ModelsCatalogResponse,
  ProviderView,
} from '@/lib/types';
import { CopyIcon, EyeIcon, RefreshIcon, TerminalIcon } from './Icons';
import { ProviderGlyph } from './ProviderIcons';

type PromptPlatform = 'universal' | 'claude' | 'codex' | 'gemini' | 'opencode' | 'curl';

function fmtTokLimit(val?: number): string {
  if (!val || val <= 0) return '';
  if (val >= 1_000_000) return `${(val / 1_000_000).toFixed(val % 1_000_000 === 0 ? 0 : 1)}M`;
  if (val >= 1_000) return `${Math.round(val / 1_000)}k`;
  return String(val);
}

export function AccessGuideAndModels({
  originURL,
  providers,
  clientKeys,
  activeScopeFilter,
  newClientSecret,
  onCopy,
  onToast,
  onUseModelInTest,
}: {
  originURL: string;
  providers: ProviderView[];
  clientKeys: ClientKeyView[];
  activeScopeFilter: string;
  newClientSecret: { name: string; scope: string; secret: string } | null;
  onCopy: (text: string, msg?: string) => void;
  onToast: (msg: string, tone?: 'ok' | 'err') => void;
  onUseModelInTest: (providerId: string, modelId: string) => void;
}) {
  const [platform, setPlatform] = useState<PromptPlatform>('universal');
  const [selectedKeyId, setSelectedKeyId] = useState<string>('auto');
  const [manualToken, setManualToken] = useState<string>('');
  const [showManualToken, setShowManualToken] = useState(false);
  const [chosenModel, setChosenModel] = useState<string>('automodel');

  const [catalog, setCatalog] = useState<ModelsCatalogResponse | null>(null);
  const [modelLoading, setModelLoading] = useState(false);
  const [modelProbe, setModelProbe] = useState(false);
  const [modelSearch, setModelSearch] = useState('');
  const [modelProviderFilter, setModelProviderFilter] = useState('all');

  useEffect(() => {
    if (newClientSecret?.secret) {
      setManualToken(newClientSecret.secret);
      setSelectedKeyId('new');
    }
  }, [newClientSecret]);

  const effectiveScope = useMemo(() => {
    if (selectedKeyId === 'new' && newClientSecret?.scope) {
      return newClientSecret.scope;
    }
    const found = clientKeys.find((k) => k.id === selectedKeyId);
    if (found) return found.scope;
    if (activeScopeFilter && activeScopeFilter !== 'all') return activeScopeFilter;
    return 'global';
  }, [selectedKeyId, newClientSecret, clientKeys, activeScopeFilter]);

  const scopeTitle = useMemo(() => {
    if (effectiveScope === 'global') return 'Global (Semua Provider)';
    if (effectiveScope.startsWith('cat:')) return `Kategori: ${effectiveScope.slice(4)}`;
    return providers.find((p) => p.id === effectiveScope)?.name || effectiveScope;
  }, [effectiveScope, providers]);

  const tokenText = useMemo(() => {
    if (manualToken.trim()) return manualToken.trim();
    if (selectedKeyId === 'new' && newClientSecret?.secret) return newClientSecret.secret;
    return 'MASUKKAN_API_KEY_KAROUTER';
  }, [manualToken, selectedKeyId, newClientSecret]);

  const loadModels = useCallback(
    async (probeUpstream: boolean) => {
      setModelLoading(true);
      setModelProbe(probeUpstream);
      try {
        const res = await fetchModelsCatalog({
          scope: effectiveScope,
          probe: probeUpstream,
        });
        setCatalog(res);
        if (probeUpstream) {
          const okProvs = res.providers.filter((p) => p.probe_ok).length;
          onToast(`Model dimuat (${okProvs}/${res.providers.length} provider)`);
        }
      } catch (e) {
        onToast(e instanceof Error ? e.message : 'Gagal cek model', 'err');
      } finally {
        setModelLoading(false);
      }
    },
    [effectiveScope, onToast],
  );

  useEffect(() => {
    void loadModels(false);
  }, [loadModels]);

  const defaultSuggestedModel = useMemo(() => {
    if (chosenModel && chosenModel !== 'automodel' && chosenModel !== 'auto') return chosenModel;
    const match = providers.find((p) => {
      if (!p.enabled) return false;
      if (effectiveScope === 'global') return true;
      if (effectiveScope.startsWith('cat:')) {
        return (p.category || 'gratis') === effectiveScope.slice(4);
      }
      return p.id === effectiveScope;
    });
    return match?.default_model || 'automodel';
  }, [chosenModel, providers, effectiveScope]);

  const promptPayload = useMemo(() => {
    const keyVal = tokenText;
    const modelId = chosenModel || 'automodel';
    const concreteModel = defaultSuggestedModel;
    const endpointsSummary = [
      `- OpenAI Chat Completions : ${originURL}/v1/chat/completions`,
      `- OpenAI Responses API    : ${originURL}/v1/responses`,
      `- Anthropic Messages API  : ${originURL}/v1/messages`,
      `- Gemini Native API       : ${originURL}/v1beta/models/${concreteModel}:generateContent`,
      `- Daftar Model Live       : ${originURL}/v1/models?probe=1`,
    ].join('\n');

    switch (platform) {
      case 'claude':
        return {
          title: 'Prompt untuk Claude Code / Claude Agent',
          envBlock: `export ANTHROPIC_BASE_URL="${originURL}"\nexport ANTHROPIC_API_KEY="${keyVal}"\nexport ANTHROPIC_MODEL="${modelId}"`,
          text: `Tolong konfigurasi Claude Code / AI coding agent di environment ini agar memakai gateway KaRouter milik saya (${scopeTitle}):

1. Pasang environment variable berikut secara persisten di shell (~/.bashrc atau ~/.zshrc / PowerShell profile) dan ~/.claude/settings.json:
   - ANTHROPIC_BASE_URL="${originURL}"
   - ANTHROPIC_API_KEY="${keyVal}"
   - ANTHROPIC_MODEL="${modelId}"
   - OPENAI_BASE_URL="${originURL}/v1"
   - OPENAI_API_KEY="${keyVal}"

2. Endpoint yang tersedia di gateway:
${endpointsSummary}

3. Cek daftar model yang tersedia dengan perintah:
   curl -s "${originURL}/v1/models?probe=1" -H "Authorization: Bearer ${keyVal}"

4. Verifikasi koneksi dengan mengirim pesan uji ke ${originURL}/v1/messages (header x-api-key: ${keyVal}, anthropic-version: 2023-06-01) dan pastikan balasannya sukses.`,
        };
      case 'codex':
        return {
          title: 'Prompt untuk Codex CLI / OpenAI Agent',
          envBlock: `export OPENAI_BASE_URL="${originURL}/v1"\nexport OPENAI_API_KEY="${keyVal}"`,
          text: `Tolong hubungkan Codex CLI / OpenAI SDK di workspace ini ke KaRouter API Gateway (${scopeTitle}):

1. Set environment variable (Bash / PowerShell) dan konfigurasi ~/.codex/config.toml agar memakai:
   - OPENAI_BASE_URL="${originURL}/v1"
   - OPENAI_API_KEY="${keyVal}"
   - model="${modelId}"

2. Endpoint kompatibel:
${endpointsSummary}

3. Cek daftar seluruh model dari upstream terlebih dulu:
   curl -s "${originURL}/v1/models?probe=1" -H "Authorization: Bearer ${keyVal}"

4. Uji POST ke ${originURL}/v1/responses atau ${originURL}/v1/chat/completions menggunakan Bearer token tersebut lalu tampilkan model aktif.`,
        };
      case 'gemini':
        return {
          title: 'Prompt untuk Gemini CLI / Google GenAI SDK',
          envBlock: `export GOOGLE_GEMINI_BASE_URL="${originURL}"\nexport GEMINI_API_KEY="${keyVal}"`,
          text: `Tolong atur Gemini CLI / Google GenAI SDK agar melewati KaRouter Gateway (${scopeTitle}):

1. Simpan environment berikut di shell profile & .env:
   - GOOGLE_GEMINI_BASE_URL="${originURL}"
   - GEMINI_API_KEY="${keyVal}"

2. Endpoint Gemini Native & OpenAI Compat:
${endpointsSummary}

3. Jalankan cek model tersedia via:
   curl -s "${originURL}/v1/models?probe=1" -H "x-goog-api-key: ${keyVal}"

4. Verifikasi pemanggilan ke ${originURL}/v1beta/models/${concreteModel}:generateContent lalu konfirmasi respons teks berhasil.`,
        };
      case 'opencode':
        return {
          title: 'Prompt untuk Cursor / Cline / OpenCode / Aider',
          envBlock: `OPENAI_BASE_URL=${originURL}/v1\nOPENAI_API_KEY=${keyVal}\nOPENAI_MODEL=${modelId}`,
          text: `Tolong instal dan hubungkan API Key KaRouter (${scopeTitle}) ke project / editor agent ini (OpenCode, Cursor, Cline, Windsurf, atau Aider):

1. Tambahkan ke .env / settings provider OpenAI-Compatible:
   - Base URL : ${originURL}/v1
   - API Key  : ${keyVal}
   - Model    : ${modelId} (atau pilih ID spesifik dari /v1/models)

2. Endpoint lengkap:
${endpointsSummary}

3. Cek ketersediaan model di server dengan:
   curl -s "${originURL}/v1/models?probe=1" -H "Authorization: Bearer ${keyVal}"

4. Tes chat completion ke POST ${originURL}/v1/chat/completions dan laporkan hasilnya.`,
        };
      case 'curl':
        return {
          title: 'Perintah cURL Endpoint & Cek Model',
          envBlock: `export KAROUTER_URL="${originURL}"\nexport KAROUTER_KEY="${keyVal}"`,
          text: `# 1. Cek model tersedia lokal (<1ms) & live upstream (?probe=1)
curl -s "${originURL}/v1/models?probe=1" \\
  -H "Authorization: Bearer ${keyVal}"

# 2. Tes OpenAI Chat Completions (model="${modelId}")
curl -s "${originURL}/v1/chat/completions" \\
  -H "Authorization: Bearer ${keyVal}" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"${modelId}","messages":[{"role":"user","content":"Hai, balas OK"}]}'

# 3. Tes Anthropic Messages (Claude Code)
curl -s "${originURL}/v1/messages" \\
  -H "x-api-key: ${keyVal}" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"${modelId}","max_tokens":128,"messages":[{"role":"user","content":"Hai, balas OK"}]}'

# 4. Tes Gemini Native
curl -s "${originURL}/v1beta/models/${concreteModel}:generateContent" \\
  -H "x-goog-api-key: ${keyVal}" \\
  -H "Content-Type: application/json" \\
  -d '{"contents":[{"parts":[{"text":"Hai, balas OK"}]}]}'`,
        };
      default:
        return {
          title: 'Prompt Universal untuk Semua AI Agent (Claude / ChatGPT / Cursor / Cline)',
          envBlock: `export OPENAI_BASE_URL="${originURL}/v1"\nexport OPENAI_API_KEY="${keyVal}"\nexport ANTHROPIC_BASE_URL="${originURL}"\nexport ANTHROPIC_API_KEY="${keyVal}"\nexport GOOGLE_GEMINI_BASE_URL="${originURL}"\nexport GEMINI_API_KEY="${keyVal}"`,
          text: `Saya punya AI Gateway KaRouter (rotasi API Key multi-provider otomatis) dengan akses [${scopeTitle}].
Tolong setup & instal API key ini di environment / coding agent saya sekarang:

• API Key  : ${keyVal}
• Scope    : ${scopeTitle}
• Model    : ${modelId} (gunakan "automodel" untuk rotasi otomatis, atau "<provider>/<model_id>" untuk model spesifik)

Endpoint Resmi:
${endpointsSummary}

Tugas yang harus kamu lakukan:
1. Export environment variable untuk semua tool yang terpasang:
   - OpenAI SDK / Codex / Cursor / OpenCode / Aider:
     OPENAI_BASE_URL="${originURL}/v1"
     OPENAI_API_KEY="${keyVal}"
   - Claude Code / Anthropic SDK:
     ANTHROPIC_BASE_URL="${originURL}"
     ANTHROPIC_API_KEY="${keyVal}"
   - Gemini CLI / Google GenAI SDK:
     GOOGLE_GEMINI_BASE_URL="${originURL}"
     GEMINI_API_KEY="${keyVal}"
2. Panggil GET ${originURL}/v1/models?probe=1 dengan Bearer token di atas untuk mengecek daftar model yang aktif.
3. Kirim 1 request test ke endpoint yang sesuai (${originURL}/v1/chat/completions atau ${originURL}/v1/messages) lalu konfirmasi koneksi berhasil.`,
        };
    }
  }, [platform, originURL, tokenText, chosenModel, defaultSuggestedModel, scopeTitle]);

  const filteredModels = useMemo(() => {
    const q = modelSearch.trim().toLowerCase();
    return (catalog?.models || []).filter((m: ModelCatalogItem) => {
      if (modelProviderFilter !== 'all' && m.provider_id !== modelProviderFilter) return false;
      if (!q) return true;
      return (
        m.id.toLowerCase().includes(q) ||
        m.routed_id.toLowerCase().includes(q) ||
        m.display_name.toLowerCase().includes(q) ||
        m.provider_name.toLowerCase().includes(q) ||
        m.owned_by.toLowerCase().includes(q) ||
        (m.description || '').toLowerCase().includes(q)
      );
    });
  }, [catalog, modelProviderFilter, modelSearch]);

  const modelProviderOptions = useMemo(() => {
    return catalog?.providers || [];
  }, [catalog]);

  return (
    <>
      <section className="neu-card" id="akses-prompt">
        <div className="card-head">
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.45rem' }}>
            <TerminalIcon size={18} />
            <h2 className="card-title">Prompt AI Agent</h2>
          </div>
          <span className="badge badge-gemini">{scopeTitle}</span>
        </div>

        <div className="two-col" style={{ marginBottom: '0.75rem' }}>
          <div className="field" style={{ marginBottom: 0 }}>
            <label htmlFor="prompt-key-pick">Target Akses</label>
            <select
              id="prompt-key-pick"
              value={selectedKeyId}
              onChange={(e) => setSelectedKeyId(e.target.value)}
            >
              <option value="auto">Sesuai Filter ({scopeTitle})</option>
              {newClientSecret ? (
                <option value="new">Key Baru: {newClientSecret.name}</option>
              ) : null}
              {clientKeys.map((ck) => (
                <option key={ck.id} value={ck.id}>
                  {ck.name} ({ck.scope})
                </option>
              ))}
            </select>
          </div>
          <div className="field" style={{ marginBottom: 0 }}>
            <label htmlFor="prompt-model-pick">Model Target</label>
            <input
              id="prompt-model-pick"
              type="text"
              value={chosenModel}
              onChange={(e) => setChosenModel(e.target.value)}
              placeholder="auto atau provider/model"
              autoCorrect="off"
              autoCapitalize="none"
              spellCheck={false}
            />
          </div>
        </div>

        <div className="field" style={{ marginBottom: '0.75rem' }}>
          <label htmlFor="prompt-token-input">API Key untuk Prompt</label>
          <div className="pw-wrap">
            <input
              id="prompt-token-input"
              type={showManualToken ? 'text' : 'password'}
              value={manualToken}
              onChange={(e) => setManualToken(e.target.value)}
              placeholder="Tempel secret kr_... (otomatis terisi saat buat key baru)"
              autoCorrect="off"
              autoCapitalize="none"
              spellCheck={false}
            />
            <button
              type="button"
              className="pw-eye"
              onClick={() => setShowManualToken((v) => !v)}
              aria-label="Tampil token"
            >
              <EyeIcon off={showManualToken} size={17} />
            </button>
          </div>
        </div>

        <div className="endpoint-mini-grid">
          {(
            [
              ['OpenAI Base', `${originURL}/v1`],
              ['Anthropic Base', originURL],
              ['Gemini Base', originURL],
              ['Cek Models', `${originURL}/v1/models?probe=1`],
            ] as Array<[string, string]>
          ).map(([lbl, urlVal]) => (
            <div key={lbl} className="endpoint-mini-card">
              <div style={{ minWidth: 0 }}>
                <div className="status-meta-label">{lbl}</div>
                <div className="mono mono-wrap" style={{ fontSize: '0.75rem' }}>
                  {urlVal}
                </div>
              </div>
              <button
                type="button"
                className="neu-btn neu-btn-sm"
                onClick={() => onCopy(urlVal, `${lbl} disalin`)}
              >
                <CopyIcon size={13} />
                <span>Salin</span>
              </button>
            </div>
          ))}
        </div>

        <div className="pill-row" style={{ marginBottom: '0.75rem' }}>
          {(
            [
              ['universal', 'Semua AI'],
              ['claude', 'Claude Code'],
              ['codex', 'Codex CLI'],
              ['gemini', 'Gemini CLI'],
              ['opencode', 'Cursor / OpenCode'],
              ['curl', 'cURL & Tes'],
            ] as Array<[PromptPlatform, string]>
          ).map(([id, label]) => (
            <button
              key={id}
              type="button"
              className={`neu-btn neu-btn-sm ${platform === id ? 'is-active' : ''}`}
              onClick={() => setPlatform(id)}
            >
              {label}
            </button>
          ))}
        </div>

        <div className="item-top" style={{ marginBottom: '0.45rem' }}>
          <span className="kpi-sub" style={{ fontWeight: 600 }}>
            {promptPayload.title}
          </span>
          <div className="item-actions">
            <button
              type="button"
              className="neu-btn neu-btn-sm"
              onClick={() => onCopy(promptPayload.envBlock, 'Env disalin')}
            >
              <CopyIcon size={14} />
              <span>Env</span>
            </button>
            <button
              type="button"
              className="neu-btn-primary neu-btn-sm"
              onClick={() => onCopy(promptPayload.text, 'Prompt disalin')}
            >
              <CopyIcon size={14} />
              <span>Salin Prompt</span>
            </button>
          </div>
        </div>

        <pre className="code-box mono">{promptPayload.text}</pre>

        <details className="acc-details" style={{ marginTop: '0.75rem' }}>
          <summary>Cara Pasang di Claude / AI Agent Lain (4 Langkah)</summary>
          <div className="acc-body">
            <div>1. Klik tombol Buat di kartu atas untuk membuat Key Akses (`kr_...`), lalu salin secret-nya.</div>
            <div>2. Tempel ke kolom API Key di atas (atau otomatis terisi setelah key baru dibuat).</div>
            <div>3. Klik Salin Prompt lalu kirim langsung ke chat AI agent (Claude Code, Cursor, Cline, Codex, atau OpenCode).</div>
            <div>4. AI agent otomatis menyimpan `BASE_URL`, memasang `API_KEY`, mengecek `/v1/models`, dan menguji endpoint.</div>
          </div>
        </details>
      </section>

      <section className="neu-card" id="akses-models">
        <div className="card-head">
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <h2 className="card-title">Model Tersedia</h2>
            <span className="badge badge-muted">{filteredModels.length} model</span>
          </div>
          <div className="pill-row">
            <button
              type="button"
              className="neu-btn neu-btn-sm"
              disabled={modelLoading}
              onClick={() => void loadModels(false)}
            >
              <RefreshIcon size={14} />
              <span>Cek Lokal</span>
            </button>
            <button
              type="button"
              className="neu-btn-primary neu-btn-sm"
              disabled={modelLoading}
              onClick={() => void loadModels(true)}
            >
              <RefreshIcon size={14} />
              <span>{modelLoading && modelProbe ? 'Cek...' : 'Probe Upstream'}</span>
            </button>
          </div>
        </div>

        {modelProviderOptions.length > 0 ? (
          <div className="pill-row" style={{ marginBottom: '0.75rem' }}>
            {modelProviderOptions.map((mp: ModelProviderSummary) => {
              const badgeCls = !mp.enabled
                ? 'badge badge-muted'
                : mp.probed
                  ? mp.probe_ok
                    ? 'badge badge-ok'
                    : 'badge badge-err'
                  : mp.eligible_keys > 0
                    ? 'badge badge-ok'
                    : 'badge badge-warn';
              const statusTxt = !mp.enabled
                ? 'Jeda'
                : mp.probed
                  ? mp.probe_ok
                    ? `${mp.probe_count} mod · ${mp.probe_latency_ms}ms`
                    : mp.probe_error || `HTTP ${mp.probe_status}`
                  : `${mp.eligible_keys}/${mp.total_keys} key`;
              return (
                <button
                  key={mp.id}
                  type="button"
                  className={`neu-btn neu-btn-sm ${modelProviderFilter === mp.id ? 'is-active' : ''}`}
                  onClick={() =>
                    setModelProviderFilter((prev) => (prev === mp.id ? 'all' : mp.id))
                  }
                >
                  <ProviderGlyph icon={mp.icon} size={14} />
                  <span>{mp.name}</span>
                  <span className={badgeCls}>{statusTxt}</span>
                </button>
              );
            })}
          </div>
        ) : null}

        <div className="two-col" style={{ marginBottom: '0.875rem' }}>
          <input
            type="search"
            aria-label="Cari model"
            placeholder="Cari ID model / provider..."
            value={modelSearch}
            onChange={(e) => setModelSearch(e.target.value)}
          />
          <select
            aria-label="Filter provider model"
            value={modelProviderFilter}
            onChange={(e) => setModelProviderFilter(e.target.value)}
          >
            <option value="all">Semua Provider</option>
            {modelProviderOptions.map((mp) => (
              <option key={mp.id} value={mp.id}>
                {mp.name} ({mp.api_type})
              </option>
            ))}
          </select>
        </div>

        <div className="list-stack">
          {filteredModels.length === 0 ? (
            <p className="kpi-sub" style={{ margin: 0 }}>
              Tidak ada model cocok. Klik Probe Upstream untuk menarik daftar live provider.
            </p>
          ) : (
            filteredModels.map((m) => {
              const inStr = fmtTokLimit(m.input_token_limit);
              const outStr = fmtTokLimit(m.output_token_limit);
              const ctxStr = fmtTokLimit(m.context_length);
              const statusBadge =
                m.status === 'ready'
                  ? { label: 'Siap', cls: 'badge badge-ok' }
                  : m.status === 'no_key'
                    ? { label: 'Tanpa Key', cls: 'badge badge-warn' }
                    : { label: 'Jeda', cls: 'badge badge-muted' };

              return (
                <div key={`${m.provider_id}:${m.id}`} className="item-card">
                  <div className="item-top">
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.45rem', minWidth: 0 }}>
                      <ProviderGlyph icon={m.provider_icon} size={18} />
                      <strong className="mono mono-wrap">{m.id}</strong>
                    </div>
                    <div className="pill-row">
                      {m.is_default ? <span className="badge badge-gemini">Default</span> : null}
                      <span className="badge badge-muted">
                        {m.source === 'upstream' ? 'Live' : m.source === 'virtual' ? 'Auto' : 'Config'}
                      </span>
                      <span className={statusBadge.cls}>{statusBadge.label}</span>
                    </div>
                  </div>

                  <div className="item-meta">
                    <span>
                      {m.provider_name} · {m.category} · {m.api_type}
                    </span>
                    <span className="mono">
                      {[
                        inStr ? `In ${inStr}` : '',
                        outStr ? `Out ${outStr}` : '',
                        ctxStr && ctxStr !== inStr ? `Ctx ${ctxStr}` : '',
                      ]
                        .filter(Boolean)
                        .join(' · ') || m.routed_id}
                    </span>
                  </div>

                  <div className="item-actions">
                    <button
                      type="button"
                      className="neu-btn neu-btn-sm"
                      onClick={() => onCopy(m.routed_id, 'ID Model disalin')}
                    >
                      <CopyIcon size={13} />
                      <span>Salin ID</span>
                    </button>
                    <button
                      type="button"
                      className="neu-btn neu-btn-sm"
                      onClick={() => {
                        setChosenModel(m.routed_id);
                        onToast(`Model ${m.routed_id} dipilih ke prompt`);
                      }}
                    >
                      <span>Pakai di Prompt</span>
                    </button>
                    {m.provider_id !== 'karouter' ? (
                      <button
                        type="button"
                        className="neu-btn neu-btn-sm"
                        onClick={() => onUseModelInTest(m.provider_id, m.id)}
                      >
                        <span>Uji</span>
                      </button>
                    ) : null}
                  </div>

                  <details className="acc-details">
                    <summary>Detail Endpoint & Spesifikasi Model</summary>
                    <div className="acc-body">
                      {m.display_name && m.display_name !== m.id ? (
                        <div>
                          <strong>Nama:</strong> {m.display_name}
                        </div>
                      ) : null}
                      {m.description ? <div>{m.description}</div> : null}
                      <div>
                        <strong>Override Prefix:</strong>{' '}
                        <code className="mono">{m.routed_id}</code>
                      </div>
                      {m.methods && m.methods.length > 0 ? (
                        <div>
                          <strong>Metode:</strong>{' '}
                          <span className="mono">{m.methods.join(', ')}</span>
                        </div>
                      ) : null}
                      <div>
                        <strong>Endpoint Akses:</strong>{' '}
                        <span className="mono">{m.protocols.join(' · ')}</span>
                      </div>
                    </div>
                  </details>
                </div>
              );
            })
          )}
        </div>
      </section>
    </>
  );
}
