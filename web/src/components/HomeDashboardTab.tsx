'use client';

import { BAR_PALETTE, fmtSessionRemain, fmtTime, fmtUptime } from '@/lib/format';
import type { BackupConfig, ProviderView, RotationMode, StatusResponse } from '@/lib/types';
import { BookOpenIcon, CopyIcon, KeyIcon, PlusIcon, RefreshIcon } from './Icons';
import { ProviderGlyph } from './ProviderIcons';

export function HomeDashboardTab({
  statusData,
  providers,
  enabledProviders,
  totalEnabledWeight,
  originURL,
  loading,
  busy,
  lastRefreshedAt,
  sessionExpiresAt,
  activeMode,
  backupMeta,
  onRefresh,
  onOpenDocs,
  onQuickRotationMode,
  onOpenProviderForm,
  onOpenAkses,
  onCopy,
}: {
  statusData: StatusResponse | null;
  providers: ProviderView[];
  enabledProviders: ProviderView[];
  totalEnabledWeight: number;
  originURL: string;
  loading: boolean;
  busy: boolean;
  lastRefreshedAt: number;
  sessionExpiresAt: number | null;
  activeMode: RotationMode;
  backupMeta: BackupConfig | undefined;
  onRefresh: () => void;
  onOpenDocs: () => void;
  onQuickRotationMode: (mode: RotationMode) => void;
  onOpenProviderForm: () => void;
  onOpenAkses: () => void;
  onCopy: (text: string, msg?: string) => void;
}) {
  const stats = statusData?.stats;
  const pool = statusData?.pool;
  const okPct =
    stats && stats.total_requests > 0
      ? Math.round((stats.total_success / stats.total_requests) * 100)
      : 100;
  const routeMsText = stats ? (stats.avg_route_us / 1000).toFixed(2) : '0.00';
  const deadKeysCount = (statusData?.keys || []).filter((k) => k.status === 'dead').length;
  const cooldownKeysCount = (statusData?.keys || []).filter((k) => k.status === 'cooldown').length;
  const activeClientCount = (statusData?.client_keys || []).filter((ck) => ck.enabled).length;

  return (
    <>
      <section className="neu-card" aria-label="Status Gateway">
        <div className="card-head">
          <div className="pill-row" style={{ alignItems: 'center' }}>
            <span className="badge badge-ok">Online</span>
            <span className="badge badge-muted">v{statusData?.version || '4.1.0'}</span>
          </div>
          <div className="pill-row">
            <button
              type="button"
              className="neu-btn neu-btn-sm"
              onClick={onRefresh}
              disabled={loading}
              aria-label="Refresh status"
            >
              <span className={loading ? 'is-spinning' : ''} style={{ display: 'inline-flex' }}>
                <RefreshIcon size={15} />
              </span>
              <span>{loading ? 'Muat...' : 'Refresh'}</span>
            </button>
            <button type="button" className="neu-btn-primary neu-btn-sm" onClick={onOpenDocs}>
              <BookOpenIcon size={15} />
              <span>Dokumentasi</span>
            </button>
          </div>
        </div>
        <div className="status-meta-grid">
          <div className="status-meta-cell">
            <span className="status-meta-label">Terakhir Refresh</span>
            <span className="status-meta-val mono">{lastRefreshedAt ? fmtTime(lastRefreshedAt) : '—'}</span>
            <span className="kpi-sub">Auto 5 detik</span>
          </div>
          <div className="status-meta-cell">
            <span className="status-meta-label">Uptime</span>
            <span className="status-meta-val mono">{fmtUptime(statusData?.uptime_sec)}</span>
            <span className="kpi-sub">Port {statusData?.config.port || '8031'}</span>
          </div>
          <div className="status-meta-cell">
            <span className="status-meta-label">Sesi JWT</span>
            <span className="status-meta-val mono">{fmtSessionRemain(sessionExpiresAt)}</span>
            <span className="kpi-sub">Auto-logout 24j</span>
          </div>
          <div className="status-meta-cell">
            <span className="status-meta-label">Backup Neon</span>
            <span className="status-meta-val">
              {backupMeta?.has_neon_url ? backupMeta.last_status || 'Siap' : 'Belum'}
            </span>
            <span className="kpi-sub">
              {backupMeta?.has_neon_url ? `Tiap ${backupMeta.interval_days}h` : 'Atur di tab Atur'}
            </span>
          </div>
        </div>
      </section>

      <section className="kpi-grid" aria-label="Ringkasan">
        <div className="kpi-card">
          <span className="kpi-label">Provider</span>
          <span className="kpi-val">{pool?.providers ?? providers.length}</span>
          <span className="kpi-sub">
            {enabledProviders.length} aktif · {providers.length - enabledProviders.length} jeda
          </span>
        </div>
        <div className="kpi-card">
          <span className="kpi-label">Key Siap</span>
          <span className="kpi-val">{pool?.eligible ?? 0}</span>
          <span className="kpi-sub">
            dari {pool?.total ?? 0} · {deadKeysCount} mati · {cooldownKeysCount} jeda
          </span>
        </div>
        <div className="kpi-card">
          <span className="kpi-label">Rotasi</span>
          <span className="kpi-val">{routeMsText} ms</span>
          <span className="kpi-sub">
            {stats?.avg_route_us ?? 0} µs RAM · {statusData?.config.key_selection || 'smart'}
          </span>
        </div>
        <div className="kpi-card">
          <span className="kpi-label">Request</span>
          <span className="kpi-val">{stats?.total_requests ?? 0}</span>
          <span className="kpi-sub">
            {okPct}% OK · {stats?.total_tokens ?? 0} tok · {stats?.total_fallbacks ?? 0} fb
          </span>
        </div>
      </section>

      <section className="neu-card">
        <div className="card-head">
          <h2 className="card-title">Mode Rotasi</h2>
          <button type="button" className="neu-btn neu-btn-sm" onClick={onOpenProviderForm}>
            <PlusIcon size={15} />
            <span>Provider</span>
          </button>
        </div>
        <div className="pill-row" style={{ marginBottom: '0.875rem' }}>
          {(
            [
              ['weighted', 'Seimbang'],
              ['priority', 'Prioritas'],
              ['model_prefix', 'Prefix'],
            ] as Array<[RotationMode, string]>
          ).map(([mode, label]) => (
            <button
              key={mode}
              type="button"
              disabled={busy}
              className={`neu-btn neu-btn-sm ${activeMode === mode ? 'is-active' : ''}`}
              onClick={() => onQuickRotationMode(mode)}
            >
              {label}
            </button>
          ))}
        </div>

        <div className="split-track" role="img" aria-label="Distribusi bobot provider">
          {enabledProviders.length > 0 ? (
            enabledProviders.map((p, idx) => {
              const pct = totalEnabledWeight > 0 ? (Math.max(1, p.weight) / totalEnabledWeight) * 100 : 0;
              return (
                <div
                  key={p.id}
                  style={{
                    width: `${pct}%`,
                    height: '100%',
                    background: BAR_PALETTE[idx % BAR_PALETTE.length],
                    transition: 'width 220ms ease',
                  }}
                />
              );
            })
          ) : (
            <div style={{ width: '100%', height: '100%', background: 'var(--neu-muted)', opacity: 0.25 }} />
          )}
        </div>

        <div className="split-meta">
          {providers.map((p, idx) => {
            const pct =
              p.enabled && totalEnabledWeight > 0
                ? Math.round((Math.max(1, p.weight) / totalEnabledWeight) * 100)
                : 0;
            return (
              <span
                key={p.id}
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: '0.35rem',
                  fontWeight: 600,
                }}
              >
                <span
                  style={{
                    width: 8,
                    height: 8,
                    borderRadius: '9999px',
                    background: p.enabled ? BAR_PALETTE[idx % BAR_PALETTE.length] : 'var(--neu-muted)',
                  }}
                />
                <ProviderGlyph icon={p.icon} size={14} />
                <span>{p.name}</span>
                <span className="badge badge-muted">{p.category || 'gratis'}</span>
                <span style={{ color: 'var(--neu-muted)' }}>
                  {pct}% · {p.eligible_keys}/{p.total_keys} key · {p.req_count} req
                </span>
              </span>
            );
          })}
        </div>
      </section>

      <section className="neu-card">
        <div className="card-head">
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.45rem' }}>
            <h2 className="card-title">Endpoint & Akses</h2>
            <span className="badge badge-ok">{activeClientCount} key aktif</span>
          </div>
          <div className="pill-row">
            <button type="button" className="neu-btn neu-btn-sm" onClick={onOpenAkses}>
              <KeyIcon size={14} />
              <span>Prompt & Model</span>
            </button>
            <button
              type="button"
              className="neu-btn neu-btn-sm"
              onClick={() =>
                onCopy(
                  `export OPENAI_BASE_URL="${originURL}/v1"\nexport ANTHROPIC_BASE_URL="${originURL}"\nexport GOOGLE_GEMINI_BASE_URL="${originURL}"`,
                  'Env disalin',
                )
              }
            >
              <CopyIcon size={14} />
              <span>Env Agent</span>
            </button>
          </div>
        </div>
        <pre className="code-box mono" style={{ marginBottom: '0.65rem' }}>{`${originURL}/v1`}</pre>
        <div className="pill-row">
          <span className="badge badge-muted mono">/v1/models</span>
          <span className="badge badge-muted mono">/v1/chat/completions</span>
          <span className="badge badge-muted mono">/v1/responses</span>
          <span className="badge badge-muted mono">/v1/messages</span>
          <span className="badge badge-muted mono">/v1beta/models</span>
        </div>
      </section>
    </>
  );
}
