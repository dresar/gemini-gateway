'use client';

import { deleteClientKey, patchClientKey } from '@/lib/api';
import { type PendingConfirm, StatusGlyph } from '@/lib/format';
import type { ClientKeyView, ProviderView } from '@/lib/types';
import { AccessGuideAndModels } from './AccessGuideAndModels';
import { CopyIcon, KeyIcon, PlusIcon } from './Icons';
import { ProviderGlyph } from './ProviderIcons';

export function AksesTab({
  originURL,
  providers,
  providerMap,
  categories,
  clientKeys,
  filteredClientKeys,
  clientScopeFilter,
  newClientSecret,
  busy,
  onSetScopeFilter,
  onOpenCreateModal,
  onDismissSecret,
  onCopy,
  onToast,
  onReload,
  onConfirm,
  onUseModelInTest,
}: {
  originURL: string;
  providers: ProviderView[];
  providerMap: Map<string, ProviderView>;
  categories: string[];
  clientKeys: ClientKeyView[];
  filteredClientKeys: ClientKeyView[];
  clientScopeFilter: string;
  newClientSecret: { name: string; scope: string; secret: string } | null;
  busy: boolean;
  onSetScopeFilter: (scope: string) => void;
  onOpenCreateModal: () => void;
  onDismissSecret: () => void;
  onCopy: (text: string, msg?: string) => void;
  onToast: (msg: string, tone?: 'ok' | 'err') => void;
  onReload: () => Promise<void>;
  onConfirm: (next: PendingConfirm | null) => void;
  onUseModelInTest: (providerId: string, modelId: string) => void;
}) {
  const formatScopeLabel = (scope: string): string => {
    if (!scope || scope === 'global') return 'Global';
    if (scope.startsWith('cat:')) return `Grup: ${scope.slice(4)}`;
    return providerMap.get(scope)?.name || scope;
  };

  return (
    <>
      <section className="neu-card">
        <div className="toolbar">
          <div className="pill-row">
            <button
              type="button"
              className={`neu-btn neu-btn-sm ${clientScopeFilter === 'all' ? 'is-active' : ''}`}
              onClick={() => onSetScopeFilter('all')}
            >
              Semua
            </button>
            <button
              type="button"
              className={`neu-btn neu-btn-sm ${clientScopeFilter === 'global' ? 'is-active' : ''}`}
              onClick={() => onSetScopeFilter('global')}
            >
              Global
            </button>
            {categories.map((cat) => {
              const val = `cat:${cat}`;
              return (
                <button
                  key={val}
                  type="button"
                  className={`neu-btn neu-btn-sm ${clientScopeFilter === val ? 'is-active' : ''}`}
                  onClick={() => onSetScopeFilter(val)}
                >
                  Grup: {cat}
                </button>
              );
            })}
            {providers.map((p) => (
              <button
                key={p.id}
                type="button"
                className={`neu-btn neu-btn-sm ${clientScopeFilter === p.id ? 'is-active' : ''}`}
                onClick={() => onSetScopeFilter(p.id)}
              >
                <ProviderGlyph icon={p.icon} size={14} />
                <span>{p.name}</span>
              </button>
            ))}
          </div>
          <button type="button" className="neu-btn-primary neu-btn-sm" onClick={onOpenCreateModal}>
            <PlusIcon size={15} />
            <span>Buat</span>
          </button>
        </div>

        {newClientSecret ? (
          <div className="secret-box" style={{ marginBottom: '0.875rem' }}>
            <div className="item-top">
              <strong>Key Baru: {newClientSecret.name}</strong>
              <div className="item-actions">
                <button
                  type="button"
                  className="neu-btn neu-btn-sm"
                  onClick={() => onCopy(newClientSecret.secret, 'Key disalin')}
                >
                  <CopyIcon size={14} />
                  <span>Salin</span>
                </button>
                <button type="button" className="neu-btn neu-btn-sm" onClick={onDismissSecret}>
                  Tutup
                </button>
              </div>
            </div>
            <code className="code-box mono-wrap">{newClientSecret.secret}</code>
          </div>
        ) : null}

        <div className="list-stack">
          {filteredClientKeys.length === 0 ? (
            <p className="kpi-sub" style={{ margin: 0 }}>
              Belum ada key akses.
            </p>
          ) : (
            filteredClientKeys.map((ck: ClientKeyView) => {
              const scopeProv = providerMap.get(ck.scope);
              return (
                <div key={ck.id} className="item-card">
                  <div className="item-top">
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', minWidth: 0 }}>
                      {scopeProv ? <ProviderGlyph icon={scopeProv.icon} size={18} /> : <KeyIcon size={16} />}
                      <strong className="mono-wrap">{ck.name}</strong>
                      <span className="badge badge-muted">{formatScopeLabel(ck.scope)}</span>
                    </div>
                    <span className={ck.enabled ? 'badge badge-ok' : 'badge badge-muted'}>
                      <StatusGlyph kind={ck.enabled ? 'ok' : 'muted'} />
                      <span>{ck.enabled ? 'Aktif' : 'Jeda'}</span>
                    </span>
                  </div>
                  <div className="item-meta">
                    <span className="mono mono-wrap">{ck.masked}</span>
                    <span>{ck.usage_count} req</span>
                  </div>
                  <div className="item-actions" style={{ alignItems: 'center' }}>
                    <select
                      aria-label={`Kategori ${ck.name}`}
                      value={ck.scope}
                      disabled={busy}
                      style={{ width: 'auto', minWidth: 130, minHeight: 38, padding: '0.3rem 0.65rem' }}
                      onChange={(e) => {
                        const nextScope = e.target.value;
                        void patchClientKey(ck.id, { scope: nextScope })
                          .then(() => onReload())
                          .then(() => onToast('Kategori diubah'))
                          .catch((err) => onToast(err instanceof Error ? err.message : 'Gagal', 'err'));
                      }}
                    >
                      <option value="global">Global</option>
                      {categories.map((cat) => (
                        <option key={`cat:${cat}`} value={`cat:${cat}`}>
                          Grup: {cat}
                        </option>
                      ))}
                      {providers.map((p) => (
                        <option key={p.id} value={p.id}>
                          {p.name}
                        </option>
                      ))}
                    </select>
                    <button
                      type="button"
                      className="neu-btn neu-btn-sm"
                      disabled={busy}
                      onClick={() => {
                        void patchClientKey(ck.id, { enabled: !ck.enabled })
                          .then(() => onReload())
                          .catch((err) => onToast(err instanceof Error ? err.message : 'Gagal', 'err'));
                      }}
                    >
                      {ck.enabled ? 'Jeda' : 'Aktif'}
                    </button>
                    <button
                      type="button"
                      className="neu-btn-danger neu-btn-sm"
                      disabled={busy}
                      onClick={() =>
                        onConfirm({
                          title: 'Hapus Akses',
                          message: `Hapus key "${ck.name}"?`,
                          confirmLabel: 'Hapus',
                          run: async () => {
                            await deleteClientKey(ck.id);
                            onConfirm(null);
                            await onReload();
                            onToast('Dihapus');
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

      <AccessGuideAndModels
        originURL={originURL}
        providers={providers}
        clientKeys={clientKeys}
        activeScopeFilter={clientScopeFilter}
        newClientSecret={newClientSecret}
        onCopy={onCopy}
        onToast={onToast}
        onUseModelInTest={onUseModelInTest}
      />
    </>
  );
}
