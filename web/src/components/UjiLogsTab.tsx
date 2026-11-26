'use client';

import type React from 'react';
import { useEffect, useMemo, useState } from 'react';
import { runChatTest, testRawKey } from '@/lib/api';
import { errorCodeLabel, fmtTime, type LogFilter, StatusGlyph } from '@/lib/format';
import type { APIType, AuthMode, ChatTestResult, KeyTestResult, ProviderView, RequestLog } from '@/lib/types';
import { EyeIcon } from './Icons';

export function UjiLogsTab({
  providers,
  providerMap,
  logs,
  sampleModel,
  jumpTarget,
  onReload,
  onToast,
}: {
  providers: ProviderView[];
  providerMap: Map<string, ProviderView>;
  logs: RequestLog[];
  sampleModel: string;
  jumpTarget: { providerId: string; modelId: string; seq: number } | null;
  onReload: () => Promise<void>;
  onToast: (msg: string, tone?: 'ok' | 'err') => void;
}) {
  const [chatTarget, setChatTarget] = useState<string>('auto');
  const [chatModel, setChatModel] = useState<string>('');
  const [chatSystem, setChatSystem] = useState<string>('');
  const [chatPrompt, setChatPrompt] = useState('Balas singkat: OK');
  const [chatTokens, setChatTokens] = useState(256);
  const [chatBusy, setChatBusy] = useState(false);
  const [chatResult, setChatResult] = useState<ChatTestResult | null>(null);

  const [rawMode, setRawMode] = useState<string>(providers[0]?.id || 'custom');
  const [rawApiKey, setRawApiKey] = useState('');
  const [showRawKey, setShowRawKey] = useState(false);
  const [rawModel, setRawModel] = useState('');
  const [rawBaseURL, setRawBaseURL] = useState('');
  const [rawApiType, setRawApiType] = useState<APIType>('openai');
  const [rawAuthMode, setRawAuthMode] = useState<AuthMode>('bearer');
  const [rawTestBusy, setRawTestBusy] = useState(false);
  const [rawTestResult, setRawTestResult] = useState<KeyTestResult | null>(null);

  const [logFilter, setLogFilter] = useState<LogFilter>('all');
  const [logSearch, setLogSearch] = useState('');
  const [expandedLogId, setExpandedLogId] = useState<number | null>(null);

  useEffect(() => {
    if (providers.length > 0 && rawMode === 'custom') {
      setRawMode(providers[0].id);
    }
  }, [providers, rawMode]);

  useEffect(() => {
    if (!jumpTarget) return;
    setChatTarget(jumpTarget.providerId);
    setChatModel(jumpTarget.modelId);
  }, [jumpTarget]);

  const filteredLogs = useMemo(() => {
    const q = logSearch.trim().toLowerCase();
    return logs.filter((lg: RequestLog) => {
      if (logFilter === 'error' && lg.status < 400) return false;
      if (logFilter === 'ok' && lg.status >= 400) return false;
      if (q) {
        const match =
          (lg.provider_name || lg.provider).toLowerCase().includes(q) ||
          (lg.upstream_model || lg.requested_model).toLowerCase().includes(q) ||
          (lg.client_name || '').toLowerCase().includes(q) ||
          (lg.error || '').toLowerCase().includes(q) ||
          (lg.error_code || '').toLowerCase().includes(q);
        if (!match) return false;
      }
      return true;
    });
  }, [logs, logFilter, logSearch]);

  const handleChatSubmit = async (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!chatPrompt.trim() || chatBusy) return;
    setChatBusy(true);
    try {
      const res = await runChatTest({
        target: chatTarget,
        model: chatModel.trim(),
        system_prompt: chatSystem.trim() || undefined,
        prompt: chatPrompt.trim(),
        max_tokens: Number(chatTokens) || 256,
      });
      setChatResult(res);
      await onReload();
      if (!res.ok) onToast(res.error || 'Uji gagal', 'err');
    } catch (err) {
      onToast(err instanceof Error ? err.message : 'Uji gagal', 'err');
    } finally {
      setChatBusy(false);
    }
  };

  const handleRawKeyTest = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!rawApiKey.trim() || rawTestBusy) {
      onToast('Isi API Key.', 'err');
      return;
    }
    setRawTestBusy(true);
    try {
      const isCustom = rawMode === 'custom';
      const res = await testRawKey(
        isCustom
          ? {
              api_key: rawApiKey.trim(),
              base_url: rawBaseURL.trim(),
              api_type: rawApiType,
              auth_mode: rawAuthMode,
              model: rawModel.trim(),
            }
          : {
              provider_id: rawMode,
              api_key: rawApiKey.trim(),
              model: rawModel.trim(),
            },
      );
      setRawTestResult(res);
      onToast(res.ok ? `OK · ${res.latency_ms} ms` : res.error || 'Key gagal', res.ok ? 'ok' : 'err');
    } catch (err) {
      onToast(err instanceof Error ? err.message : 'Tes key gagal', 'err');
    } finally {
      setRawTestBusy(false);
    }
  };

  return (
    <>
      <div className="settings-grid">
        <section className="neu-card">
          <div className="card-head">
            <h2 className="card-title">Playground</h2>
          </div>
          <form onSubmit={(e) => void handleChatSubmit(e)}>
            <div className="two-col">
              <div className="field">
                <label htmlFor="pg-target">Provider</label>
                <select id="pg-target" value={chatTarget} onChange={(e) => setChatTarget(e.target.value)}>
                  <option value="auto">Auto</option>
                  {providers.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </select>
              </div>
              <div className="field">
                <label htmlFor="pg-model">Model</label>
                <input
                  id="pg-model"
                  type="text"
                  placeholder={
                    (chatTarget !== 'auto' ? providerMap.get(chatTarget)?.default_model : sampleModel) || 'Default'
                  }
                  value={chatModel}
                  onChange={(e) => setChatModel(e.target.value)}
                />
              </div>
            </div>
            <div className="two-col">
              <div className="field">
                <label htmlFor="pg-system">System</label>
                <input
                  id="pg-system"
                  type="text"
                  placeholder="Opsional"
                  value={chatSystem}
                  onChange={(e) => setChatSystem(e.target.value)}
                />
              </div>
              <div className="field">
                <label htmlFor="pg-tokens">Token</label>
                <input
                  id="pg-tokens"
                  type="number"
                  min={16}
                  max={8192}
                  value={chatTokens}
                  onChange={(e) => setChatTokens(Number(e.target.value) || 256)}
                />
              </div>
            </div>
            <div className="field">
              <label htmlFor="pg-prompt">Prompt</label>
              <textarea
                id="pg-prompt"
                rows={3}
                value={chatPrompt}
                onChange={(e) => setChatPrompt(e.target.value)}
                onKeyDown={(e) => {
                  if ((e.metaKey || e.ctrlKey) && e.key === 'Enter' && !e.nativeEvent.isComposing) {
                    e.preventDefault();
                    void handleChatSubmit();
                  }
                }}
              />
            </div>
            <button type="submit" className="neu-btn-primary" disabled={chatBusy}>
              {chatBusy ? 'Proses...' : 'Kirim'}
            </button>
          </form>

          {chatResult ? (
            <div style={{ marginTop: '0.875rem', display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
              <div className="item-meta">
                <span className={chatResult.ok ? 'badge badge-ok' : 'badge badge-err'}>
                  <StatusGlyph kind={chatResult.ok ? 'ok' : 'err'} />
                  <span>{chatResult.ok ? 'OK' : 'Gagal'}</span>
                </span>
                <span>
                  {chatResult.provider_name || chatResult.provider || '—'} · {chatResult.latency_ms ?? 0} ms
                </span>
              </div>
              <div className="reply-box mono-wrap">
                {chatResult.ok ? chatResult.reply || '—' : chatResult.error || 'Gagal'}
              </div>
            </div>
          ) : null}
        </section>

        <section className="neu-card">
          <div className="card-head">
            <h2 className="card-title">Tes Key</h2>
          </div>
          <form onSubmit={(e) => void handleRawKeyTest(e)}>
            <div className="two-col">
              <div className="field">
                <label htmlFor="rk-mode">Provider</label>
                <select id="rk-mode" value={rawMode} onChange={(e) => setRawMode(e.target.value)}>
                  {providers.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                  <option value="custom">Custom Endpoint</option>
                </select>
              </div>
              <div className="field">
                <label htmlFor="rk-model">Model</label>
                <input
                  id="rk-model"
                  type="text"
                  placeholder={providerMap.get(rawMode)?.default_model || 'gemini-2.5-flash'}
                  value={rawModel}
                  onChange={(e) => setRawModel(e.target.value)}
                />
              </div>
            </div>

            {rawMode === 'custom' ? (
              <>
                <div className="field">
                  <label htmlFor="rk-url">Endpoint</label>
                  <input
                    id="rk-url"
                    type="url"
                    required
                    placeholder="https://api.groq.com/openai/v1"
                    value={rawBaseURL}
                    onChange={(e) => setRawBaseURL(e.target.value)}
                  />
                </div>
                <div className="two-col">
                  <div className="field">
                    <label htmlFor="rk-type">Tipe API</label>
                    <select
                      id="rk-type"
                      value={rawApiType}
                      onChange={(e) => setRawApiType(e.target.value as APIType)}
                    >
                      <option value="openai">OpenAI-Compat</option>
                      <option value="gemini">Gemini</option>
                    </select>
                  </div>
                  <div className="field">
                    <label htmlFor="rk-auth">Auth</label>
                    <select
                      id="rk-auth"
                      value={rawAuthMode}
                      onChange={(e) => setRawAuthMode(e.target.value as AuthMode)}
                    >
                      <option value="bearer">Bearer</option>
                      <option value="x-api-key">x-api-key</option>
                      <option value="query_key">Query Key</option>
                    </select>
                  </div>
                </div>
              </>
            ) : null}

            <div className="field">
              <label htmlFor="rk-key">API Key</label>
              <div className="pw-wrap">
                <input
                  id="rk-key"
                  type={showRawKey ? 'text' : 'password'}
                  required
                  autoComplete="off"
                  placeholder="Tempel key"
                  value={rawApiKey}
                  onChange={(e) => setRawApiKey(e.target.value)}
                />
                <button
                  type="button"
                  className="pw-eye"
                  onClick={() => setShowRawKey((v) => !v)}
                  aria-label="Tampil key"
                >
                  <EyeIcon off={showRawKey} size={17} />
                </button>
              </div>
            </div>
            <button type="submit" className="neu-btn-primary" disabled={rawTestBusy}>
              {rawTestBusy ? 'Menguji...' : 'Tes'}
            </button>
          </form>

          {rawTestResult ? (
            <div style={{ marginTop: '0.875rem', display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
              <div className="item-meta">
                <span className={rawTestResult.ok ? 'badge badge-ok' : 'badge badge-err'}>
                  <StatusGlyph kind={rawTestResult.ok ? 'ok' : 'err'} />
                  <span>
                    {rawTestResult.ok
                      ? `OK · ${rawTestResult.status}`
                      : `${errorCodeLabel(rawTestResult.error_code) || 'Gagal'} · ${rawTestResult.status}`}
                  </span>
                </span>
                <span>
                  {rawTestResult.model} · {rawTestResult.latency_ms} ms
                </span>
              </div>
              <div className="reply-box mono-wrap">
                {rawTestResult.ok ? rawTestResult.reply || 'OK' : rawTestResult.error || 'Gagal'}
              </div>
            </div>
          ) : null}
        </section>
      </div>

      <section className="neu-card">
        <div className="toolbar">
          <h2 className="card-title">Riwayat</h2>
          <div className="pill-row">
            {(
              [
                ['all', 'Semua'],
                ['error', 'Error'],
                ['ok', 'OK'],
              ] as Array<[LogFilter, string]>
            ).map(([val, lbl]) => (
              <button
                key={val}
                type="button"
                className={`neu-btn neu-btn-sm ${logFilter === val ? 'is-active' : ''}`}
                onClick={() => setLogFilter(val)}
              >
                {lbl}
              </button>
            ))}
          </div>
        </div>

        <div className="field">
          <input
            type="search"
            aria-label="Cari log"
            placeholder="Cari model / provider / error..."
            value={logSearch}
            onChange={(e) => setLogSearch(e.target.value)}
          />
        </div>

        <div className="list-stack">
          {filteredLogs.length === 0 ? (
            <p className="kpi-sub" style={{ margin: 0 }}>
              Belum ada log.
            </p>
          ) : (
            filteredLogs.slice(0, 40).map((lg: RequestLog) => {
              const isErr = lg.status >= 400 || Boolean(lg.error);
              const isExpanded = expandedLogId === lg.id;
              const errLbl = errorCodeLabel(lg.error_code);
              return (
                <div
                  key={lg.id}
                  className="item-card"
                  onClick={() => setExpandedLogId(isExpanded ? null : lg.id)}
                  style={{ cursor: 'pointer' }}
                >
                  <div className="item-top">
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.45rem', minWidth: 0 }}>
                      <span className="mono kpi-sub">{fmtTime(lg.ts)}</span>
                      <strong>{lg.provider_name || lg.provider}</strong>
                      <span className="mono kpi-sub">{lg.upstream_model || lg.requested_model}</span>
                    </div>
                    <div className="pill-row">
                      {errLbl ? <span className="badge badge-err">{errLbl}</span> : null}
                      <span className={isErr ? 'badge badge-err' : 'badge badge-ok'}>{lg.status}</span>
                    </div>
                  </div>
                  <div className="item-meta">
                    <span className="mono">
                      Key #{lg.key_index} {lg.key_masked || '—'} · {lg.client_name || 'client'}
                    </span>
                    <span className="mono">
                      {lg.route_us ?? 0} µs · {lg.latency_ms} ms
                    </span>
                  </div>
                  {lg.error ? (
                    <div
                      className="neu-inset-box field-err mono-wrap"
                      style={{
                        fontSize: '0.75rem',
                        maxHeight: isExpanded ? 'none' : '3.3em',
                        overflow: 'hidden',
                      }}
                    >
                      {lg.error}
                    </div>
                  ) : isExpanded ? (
                    <div className="neu-inset-box kpi-sub mono-wrap">
                      Scope: {lg.scope || 'global'} · Req: {lg.requested_model} · Up: {lg.upstream_model} · Token:{' '}
                      {lg.tokens} · Try: {lg.attempts}
                    </div>
                  ) : null}
                </div>
              );
            })
          )}
        </div>
      </section>
    </>
  );
}
