'use client';

import { deleteKey, deleteProvider, patchKey } from '@/lib/api';
import { errorCodeLabel, type PendingConfirm, statusBadgeMeta, type StatusFilter, StatusGlyph } from '@/lib/format';
import type { KeyView, ProviderView } from '@/lib/types';
import { PlusIcon } from './Icons';
import { ProviderGlyph } from './ProviderIcons';

export function PoolTab({
  providers,
  filteredProviders,
  filteredPoolKeys,
  providerMap,
  categories,
  poolCategoryFilter,
  poolProviderFilter,
  poolStatusFilter,
  poolSearch,
  busy,
  onSetCategoryFilter,
  onSetProviderFilter,
  onSetStatusFilter,
  onSetSearch,
  onOpenProviderForm,
  onOpenAddKeysModal,
  onTestPoolKey,
  onBulkPool,
  onReload,
  onToast,
  onConfirm,
}: {
  providers: ProviderView[];
  filteredProviders: ProviderView[];
  filteredPoolKeys: KeyView[];
  providerMap: Map<string, ProviderView>;
  categories: string[];
  poolCategoryFilter: string;
  poolProviderFilter: string;
  poolStatusFilter: StatusFilter;
  poolSearch: string;
  busy: boolean;
  onSetCategoryFilter: (cat: string) => void;
  onSetProviderFilter: (prov: string) => void;
  onSetStatusFilter: (st: StatusFilter) => void;
  onSetSearch: (q: string) => void;
  onOpenProviderForm: (editing: ProviderView | null) => void;
  onOpenAddKeysModal: (providerId: string) => void;
  onTestPoolKey: (id: string) => Promise<void>;
  onBulkPool: (
    action: 'revive_all' | 'purge_dead' | 'test_all' | 'dedupe_all' | 'purge_invalid' | 'auto_clean',
  ) => Promise<void>;
  onReload: () => Promise<void>;
  onToast: (msg: string, tone?: 'ok' | 'err') => void;
  onConfirm: (next: PendingConfirm | null) => void;
}) {
  return (
    <>
      <section className="neu-card">
        <div className="card-head">
          <h2 className="card-title">Provider</h2>
          <button
            type="button"
            className="neu-btn-primary neu-btn-sm"
            onClick={() => onOpenProviderForm(null)}
          >
            <PlusIcon size={15} />
            <span>Provider</span>
          </button>
        </div>
        {categories.length > 0 ? (
          <div className="pill-row" style={{ marginBottom: '0.75rem' }}>
            <button
              type="button"
              className={`neu-btn neu-btn-sm ${poolCategoryFilter === 'all' ? 'is-active' : ''}`}
              onClick={() => onSetCategoryFilter('all')}
            >
              Semua
            </button>
            {categories.map((cat) => (
              <button
                key={cat}
                type="button"
                className={`neu-btn neu-btn-sm ${poolCategoryFilter === cat ? 'is-active' : ''}`}
                onClick={() => onSetCategoryFilter(cat)}
              >
                {cat}
              </button>
            ))}
          </div>
        ) : null}
        <div className="prov-grid">
          {filteredProviders.map((p) => (
            <div key={p.id} className="item-card">
              <div className="item-top">
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', minWidth: 0 }}>
                  <ProviderGlyph icon={p.icon} size={22} />
                  <strong className="mono-wrap">{p.name}</strong>
                  <span className="badge badge-muted">{p.category || 'gratis'}</span>
                </div>
                <span className={p.enabled ? 'badge badge-ok' : 'badge badge-muted'}>
                  <StatusGlyph kind={p.enabled ? 'ok' : 'muted'} />
                  <span>{p.enabled ? 'Aktif' : 'Jeda'}</span>
                </span>
              </div>
              <div className="item-meta">
                <span className="mono mono-wrap">{p.default_model || '—'}</span>
                <span>
                  {p.eligible_keys}/{p.total_keys} key
                </span>
              </div>
              <div className="item-meta">
                <span>
                  {p.api_type.toUpperCase()} · {p.auth_mode}
                </span>
                <span>
                  W{p.weight} · P{p.priority}
                </span>
              </div>
              <div className="item-actions">
                <button
                  type="button"
                  className="neu-btn neu-btn-sm"
                  onClick={() => onOpenAddKeysModal(p.id)}
                >
                  <PlusIcon size={14} />
                  <span>Key</span>
                </button>
                <button
                  type="button"
                  className="neu-btn neu-btn-sm"
                  onClick={() => onOpenProviderForm(p)}
                >
                  Edit
                </button>
                <button
                  type="button"
                  className="neu-btn-danger neu-btn-sm"
                  disabled={busy}
                  onClick={() =>
                    onConfirm({
                      title: 'Hapus Provider',
                      message: `Hapus provider "${p.name}" beserta semua key-nya?`,
                      confirmLabel: 'Hapus',
                      run: async () => {
                        await deleteProvider(p.id);
                        onConfirm(null);
                        await onReload();
                        onToast('Provider dihapus');
                      },
                    })
                  }
                >
                  Hapus
                </button>
              </div>
            </div>
          ))}
        </div>
      </section>

      <section className="neu-card">
        <div className="toolbar">
          <div className="pill-row">
            <button
              type="button"
              className={`neu-btn neu-btn-sm ${poolProviderFilter === 'all' ? 'is-active' : ''}`}
              onClick={() => onSetProviderFilter('all')}
            >
              Semua
            </button>
            {providers.map((p) => (
              <button
                key={p.id}
                type="button"
                className={`neu-btn neu-btn-sm ${poolProviderFilter === p.id ? 'is-active' : ''}`}
                onClick={() => onSetProviderFilter(p.id)}
              >
                <ProviderGlyph icon={p.icon} size={14} />
                <span>{p.name}</span>
              </button>
            ))}
          </div>
          <div className="pill-row">
            <button
              type="button"
              className="neu-btn-primary neu-btn-sm"
              onClick={() =>
                onOpenAddKeysModal(poolProviderFilter === 'all' ? providers[0]?.id || '' : poolProviderFilter)
              }
            >
              <PlusIcon size={15} />
              <span>Tambah</span>
            </button>
            <button
              type="button"
              className="neu-btn neu-btn-sm"
              disabled={busy}
              onClick={() => void onBulkPool('test_all')}
            >
              Uji
            </button>
            <button
              type="button"
              className="neu-btn neu-btn-sm"
              disabled={busy}
              onClick={() => void onBulkPool('revive_all')}
            >
              Pulih
            </button>
            <button
              type="button"
              className="neu-btn neu-btn-sm"
              disabled={busy}
              onClick={() => void onBulkPool('auto_clean')}
            >
              Dedup
            </button>
            <button
              type="button"
              className="neu-btn-danger neu-btn-sm"
              disabled={busy}
              onClick={() =>
                onConfirm({
                  title: 'Bersih Key',
                  message: 'Hapus semua key mati di filter ini?',
                  confirmLabel: 'Hapus',
                  run: async () => {
                    await onBulkPool('purge_dead');
                    onConfirm(null);
                  },
                })
              }
            >
              Bersih
            </button>
          </div>
        </div>

        <div className="two-col" style={{ marginBottom: '0.875rem' }}>
          <input
            type="search"
            aria-label="Cari key"
            placeholder="Cari key / error..."
            value={poolSearch}
            onChange={(e) => onSetSearch(e.target.value)}
          />
          <select
            aria-label="Filter status"
            value={poolStatusFilter}
            onChange={(e) => onSetStatusFilter(e.target.value as StatusFilter)}
          >
            <option value="all">Semua</option>
            <option value="healthy">Siap</option>
            <option value="cooldown">Cooldown</option>
            <option value="dead">Mati</option>
            <option value="paused">Jeda</option>
          </select>
        </div>

        <div className="list-stack">
          {filteredPoolKeys.length === 0 ? (
            <p className="kpi-sub" style={{ margin: 0 }}>
              Tidak ada upstream key.
            </p>
          ) : (
            filteredPoolKeys.map((k) => {
              const prov = providerMap.get(k.provider);
              const meta = statusBadgeMeta(k);
              const errBadge = errorCodeLabel(k.last_error_code);
              return (
                <div key={k.id} className="item-card">
                  <div className="item-top">
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.45rem', minWidth: 0 }}>
                      <ProviderGlyph icon={prov?.icon || 'custom'} size={18} />
                      <strong className="mono-wrap">{k.label}</strong>
                      <span className="kpi-sub">{prov?.name || k.provider}</span>
                    </div>
                    <div className="pill-row">
                      {errBadge ? <span className="badge badge-err">{errBadge}</span> : null}
                      <span className={meta.cls}>
                        <StatusGlyph kind={meta.kind} />
                        <span>{meta.label}</span>
                      </span>
                    </div>
                  </div>
                  <div className="item-meta">
                    <span className="mono mono-wrap">{k.masked}</span>
                    <span>
                      {k.ok_count} OK · {k.fail_429 + k.fail_auth + k.fail_other} gagal · {k.last_latency_ms} ms
                    </span>
                  </div>
                  {k.last_error ? (
                    <div className="neu-inset-box field-err mono-wrap" style={{ fontSize: '0.75rem' }}>
                      {k.last_error}
                    </div>
                  ) : null}
                  <div className="item-actions">
                    <button
                      type="button"
                      className="neu-btn neu-btn-sm"
                      disabled={busy}
                      onClick={() => void onTestPoolKey(k.id)}
                    >
                      Uji
                    </button>
                    <button
                      type="button"
                      className="neu-btn neu-btn-sm"
                      disabled={busy}
                      onClick={() => {
                        void patchKey(k.id, { enabled: !k.enabled })
                          .then(() => onReload())
                          .catch((err) => onToast(err instanceof Error ? err.message : 'Gagal', 'err'));
                      }}
                    >
                      {k.enabled ? 'Jeda' : 'Aktif'}
                    </button>
                    {k.status === 'dead' || k.status === 'cooldown' || k.consec_fails > 0 ? (
                      <button
                        type="button"
                        className="neu-btn neu-btn-sm"
                        disabled={busy}
                        onClick={() => {
                          void patchKey(k.id, { action: 'revive' })
                            .then(() => onReload())
                            .then(() => onToast('Dipulihkan'))
                            .catch((err) => onToast(err instanceof Error ? err.message : 'Gagal', 'err'));
                        }}
                      >
                        Pulih
                      </button>
                    ) : null}
                    <button
                      type="button"
                      className="neu-btn-danger neu-btn-sm"
                      disabled={busy}
                      onClick={() =>
                        onConfirm({
                          title: 'Hapus Key',
                          message: `Hapus upstream key "${k.label}"?`,
                          confirmLabel: 'Hapus',
                          run: async () => {
                            await deleteKey(k.id);
                            onConfirm(null);
                            await onReload();
                            onToast('Key dihapus');
                          },
                        })
                      }
                    >
                      Hapus
                    </button>
                  </div>
                </div>
              );
            })
          )}
        </div>
      </section>
    </>
  );
}
