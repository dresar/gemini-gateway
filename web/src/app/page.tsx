'use client';

import type React from 'react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  addKeys,
  bulkKeyAction,
  checkSession,
  createClientKey,
  createProvider,
  fetchStatus,
  loginAdmin,
  logoutAdmin,
  saveConfig,
  testKey,
  updateProvider,
} from '@/lib/api';
import {
  applyThemeChoice,
  clipToast,
  effectiveIsDark,
  type PageView,
  type PendingConfirm,
  readStoredTheme,
  type StatusFilter,
  type TabId,
  type ToastState,
} from '@/lib/format';
import type {
  BackupConfig,
  ProviderInput,
  ProviderView,
  RotationMode,
  StatusResponse,
  ThemeChoice,
} from '@/lib/types';
import { AksesTab } from '@/components/AksesTab';
import { AturTab } from '@/components/AturTab';
import { DocsPage } from '@/components/DocsPage';
import { HomeDashboardTab } from '@/components/HomeDashboardTab';
import {
  CloseIcon,
  DashboardIcon,
  KaRouterIcon,
  KeyIcon,
  LogoutIcon,
  MoonIcon,
  ServerIcon,
  SlidersIcon,
  SunIcon,
  TerminalIcon,
} from '@/components/Icons';
import { AddKeysDialog, ConfirmDialog, CreateClientKeyDialog } from '@/components/KeyModal';
import { LoginView } from '@/components/LoginView';
import { PoolTab } from '@/components/PoolTab';
import { ProviderFormPage } from '@/components/ProviderFormPage';
import { UjiLogsTab } from '@/components/UjiLogsTab';

export default function HomePage() {
  const [sessionReady, setSessionReady] = useState(false);
  const [authenticated, setAuthenticated] = useState(false);
  const [sessionExpiresAt, setSessionExpiresAt] = useState<number | null>(null);
  const [loginPassword, setLoginPassword] = useState('');
  const [showLoginPw, setShowLoginPw] = useState(false);
  const [loginError, setLoginError] = useState('');
  const [loginBusy, setLoginBusy] = useState(false);

  const [theme, setTheme] = useState<ThemeChoice>('system');
  const [tab, setTab] = useState<TabId>('home');
  const [pageView, setPageView] = useState<PageView>(null);
  const [editingProvider, setEditingProvider] = useState<ProviderView | null>(null);

  const [statusData, setStatusData] = useState<StatusResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [lastRefreshedAt, setLastRefreshedAt] = useState<number>(0);
  const [busy, setBusy] = useState(false);

  const [addModalOpen, setAddModalOpen] = useState(false);
  const [addModalProvider, setAddModalProvider] = useState('');
  const [createClientModalOpen, setCreateClientModalOpen] = useState(false);
  const [newClientSecret, setNewClientSecret] = useState<{
    name: string;
    scope: string;
    secret: string;
  } | null>(null);
  const [pendingConfirm, setPendingConfirm] = useState<PendingConfirm | null>(null);

  const [clientScopeFilter, setClientScopeFilter] = useState<string>('all');
  const [poolCategoryFilter, setPoolCategoryFilter] = useState<string>('all');
  const [poolProviderFilter, setPoolProviderFilter] = useState<string>('all');
  const [poolStatusFilter, setPoolStatusFilter] = useState<StatusFilter>('all');
  const [poolSearch, setPoolSearch] = useState('');
  const [ujiJumpTarget, setUjiJumpTarget] = useState<{
    providerId: string;
    modelId: string;
    seq: number;
  } | null>(null);

  const [toast, setToast] = useState<ToastState | null>(null);
  const toastRef = useRef<HTMLDivElement>(null);
  const lastToastRef = useRef<{ msg: string; ts: number }>({ msg: '', ts: 0 });

  const showToast = useCallback((message: string, tone: 'ok' | 'err' = 'ok') => {
    const clean = clipToast(message);
    const now = Date.now();
    if (lastToastRef.current.msg === clean && now - lastToastRef.current.ts < 2500) {
      return;
    }
    lastToastRef.current = { msg: clean, ts: now };
    setToast({ id: now, message: clean, tone });
  }, []);

  useEffect(() => {
    const initial = readStoredTheme();
    setTheme(initial);
    applyThemeChoice(initial);
  }, []);

  useEffect(() => {
    const el = toastRef.current as (HTMLDivElement & { showPopover?: () => void; hidePopover?: () => void }) | null;
    if (!el || !toast) return;
    try {
      el.showPopover?.();
    } catch {
    }
    const t = setTimeout(() => {
      try {
        el.hidePopover?.();
      } catch {
      }
      setToast((prev) => (prev?.id === toast.id ? null : prev));
    }, 3000);
    return () => clearTimeout(t);
  }, [toast]);

  const loadStatus = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const data = await fetchStatus();
      setStatusData(data);
      setLastRefreshedAt(Math.floor(Date.now() / 1000));
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Gagal memuat status.';
      if (msg.toLowerCase().includes('unauthorized') || msg.includes('401')) {
        setAuthenticated(false);
      } else if (!silent) {
        showToast(msg, 'err');
      }
    } finally {
      if (!silent) setLoading(false);
    }
  }, [showToast]);

  useEffect(() => {
    let alive = true;
    checkSession()
      .then((res) => {
        if (!alive) return;
        setAuthenticated(res.authenticated);
        setSessionExpiresAt(res.authenticated && res.expires_at ? res.expires_at : null);
        setSessionReady(true);
        if (res.authenticated) {
          void loadStatus();
        }
      })
      .catch(() => {
        if (!alive) return;
        setAuthenticated(false);
        setSessionExpiresAt(null);
        setSessionReady(true);
      });
    return () => {
      alive = false;
    };
  }, [loadStatus]);

  useEffect(() => {
    if (!authenticated) return;
    const id = setInterval(() => {
      void loadStatus(true);
    }, 5000);
    return () => clearInterval(id);
  }, [authenticated, loadStatus]);

  useEffect(() => {
    const syncFromHash = () => {
      const h = window.location.hash.replace(/^#\/?/, '');
      if (h === 'panduan') {
        setPageView('docs');
      } else if (h === 'provider/new') {
        setEditingProvider(null);
        setPageView('provider-form');
      } else if (h === 'atur' || h === 'pool' || h === 'akses' || h === 'uji') {
        setPageView(null);
        setTab(h as TabId);
      } else if (h === '' || h === 'beranda') {
        setPageView(null);
        setTab('home');
      }
    };
    syncFromHash();
    window.addEventListener('hashchange', syncFromHash);
    return () => window.removeEventListener('hashchange', syncFromHash);
  }, []);

  const navigateTab = useCallback((nextTab: TabId) => {
    setPageView(null);
    setEditingProvider(null);
    setTab(nextTab);
    if (typeof window !== 'undefined') {
      const targetHash = nextTab === 'home' ? '#beranda' : `#${nextTab}`;
      if (window.location.hash !== targetHash) {
        window.history.replaceState(null, '', targetHash);
      }
    }
  }, []);

  const openProviderPage = useCallback((editing: ProviderView | null) => {
    setEditingProvider(editing);
    setTab('pool');
    setPageView('provider-form');
    if (typeof window !== 'undefined') {
      const nextHash = editing ? `#/provider/edit/${editing.id}` : '#/provider/new';
      window.history.pushState(null, '', nextHash);
    }
  }, []);

  const openDocsPage = useCallback(() => {
    setPageView('docs');
    if (typeof window !== 'undefined') {
      window.history.pushState(null, '', '#/panduan');
    }
  }, []);

  const providers = useMemo(() => statusData?.providers || [], [statusData]);
  const providerMap = useMemo(() => {
    const map = new Map<string, ProviderView>();
    for (const p of providers) map.set(p.id, p);
    return map;
  }, [providers]);

  const categories = useMemo(() => {
    const set = new Set<string>();
    for (const p of providers) {
      if (p.category) set.add(p.category);
    }
    return Array.from(set);
  }, [providers]);

  const filteredProviders = useMemo(() => {
    if (poolCategoryFilter === 'all') return providers;
    return providers.filter((p) => (p.category || 'gratis') === poolCategoryFilter);
  }, [providers, poolCategoryFilter]);

  const totalEnabledWeight = useMemo(
    () => providers.filter((p) => p.enabled).reduce((acc, p) => acc + Math.max(1, p.weight || 1), 0),
    [providers],
  );

  const filteredClientKeys = useMemo(() => {
    const list = statusData?.client_keys || [];
    if (clientScopeFilter === 'all') return list;
    return list.filter((c) => c.scope === clientScopeFilter);
  }, [statusData, clientScopeFilter]);

  const filteredPoolKeys = useMemo(() => {
    const q = poolSearch.trim().toLowerCase();
    return (statusData?.keys || []).filter((k) => {
      if (poolProviderFilter !== 'all' && k.provider !== poolProviderFilter) return false;
      if (poolCategoryFilter !== 'all') {
        const pCat = providerMap.get(k.provider)?.category || 'gratis';
        if (pCat !== poolCategoryFilter) return false;
      }
      if (poolStatusFilter !== 'all' && k.status !== poolStatusFilter) return false;
      if (q) {
        const provName = providerMap.get(k.provider)?.name.toLowerCase() || '';
        const match =
          k.label.toLowerCase().includes(q) ||
          k.masked.toLowerCase().includes(q) ||
          k.provider.toLowerCase().includes(q) ||
          provName.includes(q) ||
          (k.last_error || '').toLowerCase().includes(q);
        if (!match) return false;
      }
      return true;
    });
  }, [statusData, poolProviderFilter, poolCategoryFilter, poolStatusFilter, poolSearch, providerMap]);

  const handleToggleOneTapTheme = () => {
    const next: ThemeChoice = effectiveIsDark(theme) ? 'light' : 'dark';
    setTheme(next);
    applyThemeChoice(next);
  };

  const handleSelectTheme = (next: ThemeChoice) => {
    setTheme(next);
    applyThemeChoice(next);
  };

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!loginPassword) {
      setLoginError('Password wajib diisi.');
      return;
    }
    setLoginBusy(true);
    setLoginError('');
    try {
      const res = await loginAdmin(loginPassword);
      setLoginPassword('');
      setAuthenticated(true);
      setSessionExpiresAt(res.expires_at || Math.floor(Date.now() / 1000) + 86400);
      await loadStatus();
    } catch (err) {
      setLoginError(err instanceof Error ? err.message : 'Login gagal.');
    } finally {
      setLoginBusy(false);
    }
  };

  const handleLogout = useCallback(async (expired = false) => {
    await logoutAdmin().catch(() => {});
    setAuthenticated(false);
    setSessionExpiresAt(null);
    setStatusData(null);
    setNewClientSecret(null);
    setPageView(null);
    if (expired) showToast('Sesi 24 jam berakhir', 'err');
  }, [showToast]);

  useEffect(() => {
    if (!authenticated || !sessionExpiresAt) return;
    const checkExpiry = () => {
      if (Math.floor(Date.now() / 1000) >= sessionExpiresAt) {
        void handleLogout(true);
      }
    };
    checkExpiry();
    const id = setInterval(checkExpiry, 15000);
    return () => clearInterval(id);
  }, [authenticated, sessionExpiresAt, handleLogout]);

  const copyText = useCallback(async (val: string, msg = 'Disalin') => {
    try {
      await navigator.clipboard.writeText(val);
      showToast(msg);
    } catch {
      showToast('Gagal salin', 'err');
    }
  }, [showToast]);

  const handleQuickRotationMode = async (mode: RotationMode) => {
    if (!statusData) return;
    setBusy(true);
    try {
      await saveConfig({ ...statusData.config, rotation_mode: mode });
      await loadStatus(true);
      showToast('Tersimpan');
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Gagal simpan', 'err');
    } finally {
      setBusy(false);
    }
  };

  const handleSaveProvider = async (
    payload: ProviderInput,
    initialKeysRaw: string,
    editingId?: string,
  ) => {
    setBusy(true);
    try {
      let targetProvId = editingId || '';
      if (editingId) {
        const res = await updateProvider(editingId, payload);
        targetProvId = res.provider.id;
        showToast('Provider diperbarui');
      } else {
        const res = await createProvider(payload);
        targetProvId = res.provider.id;
        showToast('Provider ditambahkan');
      }
      if (initialKeysRaw.trim() && targetProvId) {
        const resKeys = await addKeys({
          provider: targetProvId,
          raw_text: initialKeysRaw,
          label_prefix: targetProvId,
        });
        showToast(`Provider & ${resKeys.added} key disimpan`);
      }
      navigateTab('pool');
      await loadStatus(true);
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Gagal simpan provider', 'err');
    } finally {
      setBusy(false);
    }
  };

  const handleAddKeys = async (payload: {
    provider: string;
    raw_text?: string;
    label_prefix?: string;
    items?: Array<{ key: string; label?: string }>;
  }) => {
    setBusy(true);
    try {
      const res = await addKeys(payload);
      setAddModalOpen(false);
      await loadStatus(true);
      const parts = [`${res.added} ditambah`];
      if (res.duplicates > 0) parts.push(`${res.duplicates} duplikat`);
      if ((res.invalid ?? 0) > 0) parts.push(`${res.invalid} invalid ditolak`);
      showToast(parts.join(' · '), res.added === 0 && (res.invalid ?? 0) > 0 ? 'err' : 'ok');
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Gagal tambah key', 'err');
    } finally {
      setBusy(false);
    }
  };

  const handleCreateClientKey = async (payload: { name: string; scope: string; custom_key?: string }) => {
    setBusy(true);
    try {
      const res = await createClientKey(payload);
      setCreateClientModalOpen(false);
      setNewClientSecret({ name: res.key.name, scope: res.key.scope, secret: res.secret });
      await loadStatus(true);
      showToast('Akses dibuat');
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Gagal buat akses', 'err');
    } finally {
      setBusy(false);
    }
  };

  const handleTestPoolKey = async (id: string) => {
    setBusy(true);
    try {
      const res = await testKey(id);
      await loadStatus(true);
      showToast(
        res.ok ? `OK · ${res.latency_ms} ms` : res.error || `Gagal (${res.status})`,
        res.ok ? 'ok' : 'err',
      );
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Gagal uji key', 'err');
    } finally {
      setBusy(false);
    }
  };

  const handleBulkPool = async (
    action: 'revive_all' | 'purge_dead' | 'test_all' | 'dedupe_all' | 'purge_invalid' | 'auto_clean',
  ) => {
    setBusy(true);
    try {
      const res = await bulkKeyAction({ action, provider: poolProviderFilter });
      await loadStatus(true);
      if (action === 'test_all') {
        showToast(`Uji: ${res.passed ?? 0} OK, ${res.failed ?? 0} gagal`);
      } else if (action === 'auto_clean' || action === 'dedupe_all' || action === 'purge_invalid') {
        showToast(res.affected > 0 ? `${res.affected} duplikat/invalid dibersihkan` : 'Pool bersih · 0 duplikat');
      } else {
        showToast(`${res.affected} key diproses`);
      }
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Aksi gagal', 'err');
    } finally {
      setBusy(false);
    }
  };

  const ThemeToggleBtn = (
    <button
      type="button"
      className="neu-icon-btn"
      onClick={handleToggleOneTapTheme}
      aria-label="Ubah tema"
      title="Tema"
    >
      <span className="theme-icon-light">
        <MoonIcon size={18} />
      </span>
      <span className="theme-icon-dark">
        <SunIcon size={18} />
      </span>
    </button>
  );

  if (!sessionReady) {
    return (
      <div className="auth-screen">
        <div className="auth-card">
          <div className="brand" style={{ justifyContent: 'center' }}>
            <KaRouterIcon size={28} />
            <h1 className="brand-name">KaRouter</h1>
          </div>
        </div>
      </div>
    );
  }

  if (!authenticated) {
    return (
      <LoginView
        password={loginPassword}
        showPassword={showLoginPw}
        error={loginError}
        busy={loginBusy}
        themeToggleBtn={ThemeToggleBtn}
        onChangePassword={(next) => {
          setLoginPassword(next);
          if (loginError) setLoginError('');
        }}
        onToggleShowPassword={() => setShowLoginPw((v) => !v)}
        onSubmit={handleLogin}
      />
    );
  }

  const originURL = typeof window !== 'undefined' ? window.location.origin : 'http://127.0.0.1:8031';
  const enabledProviders = providers.filter((p) => p.enabled);
  const sampleModel = enabledProviders[0]?.default_model || 'auto';
  const activeMode = statusData?.config.rotation_mode || 'weighted';
  const backupMeta: BackupConfig | undefined = statusData?.backup;

  const navItems: Array<{ id: TabId; label: string; icon: React.ReactNode }> = [
    { id: 'home', label: 'Beranda', icon: <DashboardIcon size={18} /> },
    { id: 'akses', label: 'Akses', icon: <KeyIcon size={18} /> },
    { id: 'pool', label: 'Pool', icon: <ServerIcon size={18} /> },
    { id: 'uji', label: 'Uji', icon: <TerminalIcon size={18} /> },
    { id: 'atur', label: 'Atur', icon: <SlidersIcon size={18} /> },
  ];

  return (
    <div className="shell">
      <header className="topbar">
        <div className="topbar-inner">
          <button
            type="button"
            className="brand"
            onClick={() => navigateTab('home')}
            aria-label="Beranda KaRouter"
          >
            <KaRouterIcon size={24} />
            <span className="brand-name">KaRouter</span>
          </button>

          <nav className="desktop-nav" aria-label="Navigasi">
            {navItems.map((item) => {
              const isCur = pageView === null && tab === item.id;
              return (
                <button
                  key={item.id}
                  type="button"
                  className={`neu-btn neu-btn-sm ${isCur ? 'is-active' : ''}`}
                  aria-current={isCur ? 'page' : undefined}
                  onClick={() => navigateTab(item.id)}
                >
                  {item.icon}
                  <span>{item.label}</span>
                </button>
              );
            })}
          </nav>

          <div className="top-actions">
            {ThemeToggleBtn}
            <button
              type="button"
              className="neu-icon-btn"
              onClick={() => void handleLogout()}
              aria-label="Keluar"
              title="Keluar"
            >
              <LogoutIcon size={17} />
            </button>
          </div>
        </div>
      </header>

      <main className="main-wrap">
        {pageView === 'provider-form' ? (
          <ProviderFormPage
            editing={editingProvider}
            busy={busy}
            existingCategories={categories}
            onCancel={() => navigateTab('pool')}
            onSubmit={handleSaveProvider}
          />
        ) : pageView === 'docs' ? (
          <DocsPage
            originURL={originURL}
            providers={providers}
            sampleModel={sampleModel}
            onBack={() => navigateTab(tab)}
            onCopy={(txt, msg) => void copyText(txt, msg)}
          />
        ) : null}

        {pageView === null && tab === 'home' ? (
          <HomeDashboardTab
            statusData={statusData}
            providers={providers}
            enabledProviders={enabledProviders}
            totalEnabledWeight={totalEnabledWeight}
            originURL={originURL}
            loading={loading}
            busy={busy}
            lastRefreshedAt={lastRefreshedAt}
            sessionExpiresAt={sessionExpiresAt}
            activeMode={activeMode}
            backupMeta={backupMeta}
            onRefresh={() => void loadStatus()}
            onOpenDocs={openDocsPage}
            onQuickRotationMode={(m) => void handleQuickRotationMode(m)}
            onOpenProviderForm={() => openProviderPage(null)}
            onOpenAkses={() => navigateTab('akses')}
            onCopy={(txt, msg) => void copyText(txt, msg)}
          />
        ) : null}

        {pageView === null && tab === 'akses' ? (
          <AksesTab
            originURL={originURL}
            providers={providers}
            providerMap={providerMap}
            categories={categories}
            clientKeys={statusData?.client_keys || []}
            filteredClientKeys={filteredClientKeys}
            clientScopeFilter={clientScopeFilter}
            newClientSecret={newClientSecret}
            busy={busy}
            onSetScopeFilter={setClientScopeFilter}
            onOpenCreateModal={() => setCreateClientModalOpen(true)}
            onDismissSecret={() => setNewClientSecret(null)}
            onCopy={(txt, msg) => void copyText(txt, msg)}
            onToast={showToast}
            onReload={() => loadStatus(true)}
            onConfirm={setPendingConfirm}
            onUseModelInTest={(providerId, modelId) => {
              setUjiJumpTarget({ providerId, modelId, seq: Date.now() });
              navigateTab('uji');
            }}
          />
        ) : null}

        {pageView === null && tab === 'pool' ? (
          <PoolTab
            providers={providers}
            filteredProviders={filteredProviders}
            filteredPoolKeys={filteredPoolKeys}
            providerMap={providerMap}
            categories={categories}
            poolCategoryFilter={poolCategoryFilter}
            poolProviderFilter={poolProviderFilter}
            poolStatusFilter={poolStatusFilter}
            poolSearch={poolSearch}
            busy={busy}
            onSetCategoryFilter={setPoolCategoryFilter}
            onSetProviderFilter={setPoolProviderFilter}
            onSetStatusFilter={setPoolStatusFilter}
            onSetSearch={setPoolSearch}
            onOpenProviderForm={openProviderPage}
            onOpenAddKeysModal={(providerId) => {
              setAddModalProvider(providerId);
              setAddModalOpen(true);
            }}
            onTestPoolKey={handleTestPoolKey}
            onBulkPool={handleBulkPool}
            onReload={() => loadStatus(true)}
            onToast={showToast}
            onConfirm={setPendingConfirm}
          />
        ) : null}

        {pageView === null && tab === 'uji' ? (
          <UjiLogsTab
            providers={providers}
            providerMap={providerMap}
            logs={statusData?.logs || []}
            sampleModel={sampleModel}
            jumpTarget={ujiJumpTarget}
            onReload={() => loadStatus(true)}
            onToast={showToast}
          />
        ) : null}

        {pageView === null && tab === 'atur' ? (
          <AturTab
            statusData={statusData}
            backupMeta={backupMeta}
            theme={theme}
            busy={busy}
            onSetBusy={setBusy}
            onSelectTheme={handleSelectTheme}
            onSessionExtended={(exp) => setSessionExpiresAt(exp)}
            onSessionExpired={() => handleLogout(true)}
            onReload={() => loadStatus(true)}
            onToast={showToast}
            onConfirm={setPendingConfirm}
          />
        ) : null}
      </main>

      <nav className="bottom-nav" aria-label="Menu">
        <div className="bottom-nav-inner">
          {navItems.map((item) => {
            const isCur = pageView === null && tab === item.id;
            return (
              <button
                key={item.id}
                type="button"
                className="bottom-tab"
                aria-current={isCur ? 'page' : undefined}
                onClick={() => navigateTab(item.id)}
              >
                {item.icon}
                <span>{item.label}</span>
              </button>
            );
          })}
        </div>
      </nav>

      <AddKeysDialog
        open={addModalOpen}
        providers={providers}
        existingKeys={statusData?.keys || []}
        defaultProvider={addModalProvider}
        busy={busy}
        onClose={() => setAddModalOpen(false)}
        onSubmit={handleAddKeys}
      />

      <CreateClientKeyDialog
        open={createClientModalOpen}
        providers={providers}
        busy={busy}
        onClose={() => setCreateClientModalOpen(false)}
        onSubmit={handleCreateClientKey}
      />

      <ConfirmDialog
        open={Boolean(pendingConfirm)}
        title={pendingConfirm?.title || 'Konfirmasi'}
        message={pendingConfirm?.message || ''}
        confirmLabel={pendingConfirm?.confirmLabel || 'Lanjut'}
        busy={busy}
        onCancel={() => setPendingConfirm(null)}
        onConfirm={async () => {
          if (pendingConfirm) await pendingConfirm.run();
        }}
      />

      {toast ? (
        <div
          ref={toastRef}
          popover="manual"
          role="status"
          className="toast-item"
          data-tone={toast.tone}
        >
          <span>{toast.message}</span>
          <button
            type="button"
            className="pw-eye"
            style={{ position: 'static' }}
            aria-label="Tutup"
            onClick={() => setToast(null)}
          >
            <CloseIcon size={15} />
          </button>
        </div>
      ) : null}
    </div>
  );
}
