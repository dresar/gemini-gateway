'use client';

import type React from 'react';
import { useCallback, useEffect, useState } from 'react';
import {
  changeAdminPassword,
  listNeonBackups,
  restoreNeonBackup,
  runNeonBackup,
  saveBackupConfig,
  saveConfig,
  testNeonConnection,
} from '@/lib/api';
import { fmtDateTime, type PendingConfirm, StatusGlyph } from '@/lib/format';
import type {
  BackupConfig,
  NeonBackupMeta,
  RotationMode,
  RouterConfig,
  StatusResponse,
  ThemeChoice,
} from '@/lib/types';
import {
  DatabaseIcon,
  DownloadIcon,
  EyeIcon,
  MonitorIcon,
  MoonIcon,
  ShieldKeyIcon,
  SmartphoneIcon,
  SunIcon,
} from './Icons';

export function AturTab({
  statusData,
  backupMeta,
  theme,
  busy,
  onSetBusy,
  onSelectTheme,
  onSessionExtended,
  onSessionExpired,
  onReload,
  onToast,
  onConfirm,
}: {
  statusData: StatusResponse | null;
  backupMeta: BackupConfig | undefined;
  theme: ThemeChoice;
  busy: boolean;
  onSetBusy: (v: boolean) => void;
  onSelectTheme: (next: ThemeChoice) => void;
  onSessionExtended: (exp: number) => void;
  onSessionExpired: () => Promise<void>;
  onReload: () => Promise<void>;
  onToast: (msg: string, tone?: 'ok' | 'err') => void;
  onConfirm: (next: PendingConfirm | null) => void;
}) {
  const [cfgDraft, setCfgDraft] = useState<RouterConfig | null>(statusData?.config || null);
  const [neonURLDraft, setNeonURLDraft] = useState('');
  const [showNeonURL, setShowNeonURL] = useState(false);
  const [backupAutoDraft, setBackupAutoDraft] = useState(backupMeta?.auto_backup ?? true);
  const [backupIntervalDraft, setBackupIntervalDraft] = useState(backupMeta?.interval_days || 7);
  const [backupBusy, setBackupBusy] = useState(false);
  const [snapshots, setSnapshots] = useState<NeonBackupMeta[]>([]);
  const [selectedSnapshotId, setSelectedSnapshotId] = useState<number>(0);

  const [currentPw, setCurrentPw] = useState('');
  const [newPw, setNewPw] = useState('');
  const [showCurrentPw, setShowCurrentPw] = useState(false);
  const [showNewPw, setShowNewPw] = useState(false);
  const [apkDownloading, setApkDownloading] = useState(false);

  useEffect(() => {
    if (statusData?.config && !cfgDraft) {
      setCfgDraft(statusData.config);
    }
    if (statusData?.backup) {
      setBackupAutoDraft(statusData.backup.auto_backup);
      setBackupIntervalDraft(statusData.backup.interval_days || 7);
    }
  }, [statusData, cfgDraft]);

  const handleLoadSnapshots = useCallback(async () => {
    try {
      const res = await listNeonBackups();
      setSnapshots(res.backups || []);
      if (res.backups?.length) {
        setSelectedSnapshotId((prev) => prev || res.backups[0].id);
      }
    } catch {
    }
  }, []);

  useEffect(() => {
    if (backupMeta?.has_neon_url) {
      void handleLoadSnapshots();
    }
  }, [backupMeta?.has_neon_url, handleLoadSnapshots]);

  const handleSaveSettings = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!cfgDraft) return;
    onSetBusy(true);
    try {
      const res = await saveConfig(cfgDraft);
      setCfgDraft(res.config);
      await onReload();
      onToast('Tersimpan');
    } catch (err) {
      onToast(err instanceof Error ? err.message : 'Gagal simpan', 'err');
    } finally {
      onSetBusy(false);
    }
  };

  const handleSaveBackup = async (e: React.FormEvent) => {
    e.preventDefault();
    setBackupBusy(true);
    try {
      await saveBackupConfig({
        neon_url: neonURLDraft.trim() || undefined,
        auto_backup: backupAutoDraft,
        interval_days: Math.max(1, Number(backupIntervalDraft) || 7),
      });
      setNeonURLDraft('');
      await onReload();
      await handleLoadSnapshots();
      onToast('Backup disimpan');
    } catch (err) {
      onToast(err instanceof Error ? err.message : 'Gagal simpan Neon', 'err');
    } finally {
      setBackupBusy(false);
    }
  };

  const handleTestNeon = async () => {
    setBackupBusy(true);
    try {
      const res = await testNeonConnection(neonURLDraft.trim() || undefined);
      onToast(res.ok ? 'Neon terhubung' : res.error || 'Koneksi gagal', res.ok ? 'ok' : 'err');
      await handleLoadSnapshots();
    } catch (err) {
      onToast(err instanceof Error ? err.message : 'Koneksi gagal', 'err');
    } finally {
      setBackupBusy(false);
    }
  };

  const handleRunBackup = async () => {
    setBackupBusy(true);
    try {
      const res = await runNeonBackup();
      await onReload();
      await handleLoadSnapshots();
      onToast(`Backup #${res.snapshot.id} sukses`);
    } catch (err) {
      onToast(err instanceof Error ? err.message : 'Backup gagal', 'err');
    } finally {
      setBackupBusy(false);
    }
  };

  const handleChangePassword = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!currentPw || newPw.length < 4) {
      onToast('Minimal 4 karakter.', 'err');
      return;
    }
    onSetBusy(true);
    try {
      const res = await changeAdminPassword(currentPw, newPw);
      if (res.expires_at) onSessionExtended(res.expires_at);
      setCurrentPw('');
      setNewPw('');
      onToast('Password diubah');
    } catch (err) {
      onToast(err instanceof Error ? err.message : 'Gagal ubah password', 'err');
    } finally {
      onSetBusy(false);
    }
  };

  const handleDownloadAPK = async () => {
    setApkDownloading(true);
    try {
      const res = await fetch('/api/app/apk', { credentials: 'same-origin', cache: 'no-store' });
      if (res.status === 401) {
        await onSessionExpired();
        return;
      }
      if (!res.ok) {
        let msg = 'APK belum tersedia di server';
        try {
          const errJson = (await res.json()) as { error?: { message?: string } };
          if (errJson?.error?.message) msg = errJson.error.message;
        } catch {
        }
        onToast(msg, 'err');
        return;
      }
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = statusData?.apk?.filename || 'karouter.apk';
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
      onToast('Mengunduh APK');
    } catch (err) {
      onToast(err instanceof Error ? err.message : 'Unduh APK gagal', 'err');
    } finally {
      setApkDownloading(false);
    }
  };

  return (
    <div className="settings-grid">
      <section className="neu-card">
        <div className="card-head">
          <h2 className="card-title">Rotasi</h2>
        </div>
        {cfgDraft ? (
          <form onSubmit={(e) => void handleSaveSettings(e)}>
            <div className="two-col">
              <div className="field">
                <label htmlFor="cfg-mode">Mode</label>
                <select
                  id="cfg-mode"
                  value={cfgDraft.rotation_mode}
                  onChange={(e) =>
                    setCfgDraft({ ...cfgDraft, rotation_mode: e.target.value as RotationMode })
                  }
                >
                  <option value="weighted">Seimbang</option>
                  <option value="priority">Prioritas</option>
                  <option value="model_prefix">Prefix</option>
                </select>
              </div>
              <div className="field">
                <label htmlFor="cfg-policy">Model Masuk</label>
                <select
                  id="cfg-policy"
                  value={cfgDraft.model_routing_policy}
                  onChange={(e) =>
                    setCfgDraft({
                      ...cfgDraft,
                      model_routing_policy: e.target.value as RouterConfig['model_routing_policy'],
                    })
                  }
                >
                  <option value="auto_or_override">Auto</option>
                  <option value="always_rotate">Rotasi</option>
                </select>
              </div>
            </div>

            <div className="two-col">
              <div className="field">
                <label htmlFor="cfg-keysel">Seleksi Key</label>
                <select
                  id="cfg-keysel"
                  value={cfgDraft.key_selection}
                  onChange={(e) =>
                    setCfgDraft({
                      ...cfgDraft,
                      key_selection: e.target.value as RouterConfig['key_selection'],
                    })
                  }
                >
                  <option value="smart">Otomatis</option>
                  <option value="random">Acak</option>
                  <option value="lru_jitter">LRU</option>
                  <option value="round_robin">Giliran</option>
                </select>
              </div>
              <div className="field">
                <label htmlFor="cfg-retries">Maks Retry</label>
                <input
                  id="cfg-retries"
                  type="number"
                  min={1}
                  max={20}
                  value={cfgDraft.max_retries_per_provider}
                  onChange={(e) =>
                    setCfgDraft({
                      ...cfgDraft,
                      max_retries_per_provider: Number(e.target.value) || 3,
                    })
                  }
                />
              </div>
            </div>

            <div className="two-col">
              <div className="field">
                <label htmlFor="cfg-cool">Cooldown (d)</label>
                <input
                  id="cfg-cool"
                  type="number"
                  min={5}
                  max={3600}
                  value={cfgDraft.cooldown_base_sec}
                  onChange={(e) =>
                    setCfgDraft({ ...cfgDraft, cooldown_base_sec: Number(e.target.value) || 60 })
                  }
                />
              </div>
              <div className="field">
                <label htmlFor="cfg-timeout">Timeout (d)</label>
                <input
                  id="cfg-timeout"
                  type="number"
                  min={10}
                  max={600}
                  value={cfgDraft.request_timeout_sec}
                  onChange={(e) =>
                    setCfgDraft({ ...cfgDraft, request_timeout_sec: Number(e.target.value) || 120 })
                  }
                />
              </div>
            </div>

            <label className="switch-row">
              <span>Auto Fallback</span>
              <input
                type="checkbox"
                checked={cfgDraft.cross_fallback}
                onChange={(e) => setCfgDraft({ ...cfgDraft, cross_fallback: e.target.checked })}
              />
            </label>

            <div style={{ marginTop: '0.75rem' }}>
              <button type="submit" className="neu-btn-primary" disabled={busy}>
                Simpan
              </button>
            </div>
          </form>
        ) : null}
      </section>

      <section className="neu-card">
        <div className="card-head">
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.45rem' }}>
            <DatabaseIcon size={18} />
            <h2 className="card-title">Backup Neon</h2>
          </div>
          <span className={backupMeta?.has_neon_url ? 'badge badge-ok' : 'badge badge-muted'}>
            <StatusGlyph kind={backupMeta?.has_neon_url ? 'ok' : 'muted'} />
            <span>{backupMeta?.has_neon_url ? 'Aktif' : 'Belum'}</span>
          </span>
        </div>

        <form onSubmit={(e) => void handleSaveBackup(e)}>
          <div className="field">
            <label htmlFor="neon-url">Neon URL</label>
            <div className="pw-wrap">
              <input
                id="neon-url"
                type={showNeonURL ? 'text' : 'password'}
                autoComplete="off"
                placeholder={
                  backupMeta?.has_neon_url
                    ? backupMeta.neon_url_masked || 'Tersimpan'
                    : 'postgresql://user:pass@host/db?sslmode=require'
                }
                value={neonURLDraft}
                onChange={(e) => setNeonURLDraft(e.target.value)}
              />
              <button
                type="button"
                className="pw-eye"
                onClick={() => setShowNeonURL((v) => !v)}
                aria-label="Tampil URL"
              >
                <EyeIcon off={showNeonURL} size={17} />
              </button>
            </div>
          </div>

          <div className="two-col">
            <div className="field">
              <label htmlFor="neon-interval">Interval (hari)</label>
              <input
                id="neon-interval"
                type="number"
                min={1}
                max={90}
                value={backupIntervalDraft}
                onChange={(e) => setBackupIntervalDraft(Math.max(1, Number(e.target.value) || 7))}
              />
            </div>
            <div className="field" style={{ justifyContent: 'flex-end' }}>
              <label className="switch-row">
                <span>Auto Backup</span>
                <input
                  type="checkbox"
                  checked={backupAutoDraft}
                  onChange={(e) => setBackupAutoDraft(e.target.checked)}
                />
              </label>
            </div>
          </div>

          {backupMeta?.last_summary || backupMeta?.last_error ? (
            <div className="neu-inset-box" style={{ marginBottom: '0.875rem', fontSize: '0.8125rem' }}>
              <div>Terakhir: {fmtDateTime(backupMeta.last_backup_unix)}</div>
              <div>Berikutnya: {fmtDateTime(backupMeta.next_backup_unix)}</div>
              {backupMeta.last_summary ? (
                <div style={{ color: 'var(--ok)', fontWeight: 600 }}>{backupMeta.last_summary}</div>
              ) : null}
              {backupMeta.last_error ? (
                <div className="field-err mono-wrap">{backupMeta.last_error}</div>
              ) : null}
            </div>
          ) : null}

          <div className="item-actions" style={{ marginBottom: '0.875rem' }}>
            <button type="submit" className="neu-btn-primary neu-btn-sm" disabled={backupBusy}>
              Simpan
            </button>
            <button
              type="button"
              className="neu-btn neu-btn-sm"
              disabled={backupBusy}
              onClick={() => void handleTestNeon()}
            >
              Tes
            </button>
            <button
              type="button"
              className="neu-btn neu-btn-sm"
              disabled={backupBusy}
              onClick={() => void handleRunBackup()}
            >
              Backup
            </button>
          </div>
        </form>

        <div className="two-col" style={{ alignItems: 'center' }}>
          <select
            aria-label="Snapshot"
            value={selectedSnapshotId || ''}
            onChange={(e) => setSelectedSnapshotId(Number(e.target.value) || 0)}
          >
            {snapshots.length === 0 ? (
              <option value="">Belum ada snapshot</option>
            ) : (
              snapshots.map((sn) => (
                <option key={sn.id} value={sn.id}>
                  #{sn.id} · {fmtDateTime(sn.created_at)} · {sn.provider_keys_count} key
                </option>
              ))
            )}
          </select>
          <div className="item-actions">
            <button
              type="button"
              className="neu-btn neu-btn-sm"
              disabled={backupBusy}
              onClick={() => void handleLoadSnapshots()}
            >
              Muat
            </button>
            <button
              type="button"
              className="neu-btn-danger neu-btn-sm"
              disabled={backupBusy || !selectedSnapshotId}
              onClick={() =>
                onConfirm({
                  title: 'Restore Backup',
                  message: `Pulihkan snapshot #${selectedSnapshotId} ke SQLite?`,
                  confirmLabel: 'Restore',
                  run: async () => {
                    await restoreNeonBackup(selectedSnapshotId);
                    onConfirm(null);
                    await onReload();
                    onToast('Restore sukses');
                  },
                })
              }
            >
              Restore
            </button>
          </div>
        </div>
      </section>

      <section className="neu-card">
        <div className="card-head">
          <h2 className="card-title">Tema</h2>
        </div>
        <div className="seg-grid-3" role="radiogroup" aria-label="Pilih tema">
          {(
            [
              ['system', 'Sistem', <MonitorIcon key="m" size={18} />],
              ['light', 'Terang', <SunIcon key="s" size={18} />],
              ['dark', 'Gelap', <MoonIcon key="d" size={18} />],
            ] as Array<[ThemeChoice, string, React.ReactNode]>
          ).map(([val, label, icon]) => (
            <label key={val} className={`seg-option ${theme === val ? 'is-selected' : ''}`}>
              <input type="radio" name="theme" checked={theme === val} onChange={() => onSelectTheme(val)} />
              {icon}
              <span>{label}</span>
            </label>
          ))}
        </div>
      </section>

      <section className="neu-card">
        <div className="card-head">
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.45rem' }}>
            <ShieldKeyIcon size={18} />
            <h2 className="card-title">Keamanan</h2>
          </div>
        </div>
        <form onSubmit={(e) => void handleChangePassword(e)}>
          <div className="field">
            <label htmlFor="pw-old">Password Lama</label>
            <div className="pw-wrap">
              <input
                id="pw-old"
                type={showCurrentPw ? 'text' : 'password'}
                autoComplete="current-password"
                value={currentPw}
                onChange={(e) => setCurrentPw(e.target.value)}
              />
              <button
                type="button"
                className="pw-eye"
                onClick={() => setShowCurrentPw((v) => !v)}
                aria-label="Tampil"
              >
                <EyeIcon off={showCurrentPw} size={17} />
              </button>
            </div>
          </div>
          <div className="field">
            <label htmlFor="pw-new">Password Baru</label>
            <div className="pw-wrap">
              <input
                id="pw-new"
                type={showNewPw ? 'text' : 'password'}
                autoComplete="new-password"
                value={newPw}
                onChange={(e) => setNewPw(e.target.value)}
              />
              <button
                type="button"
                className="pw-eye"
                onClick={() => setShowNewPw((v) => !v)}
                aria-label="Tampil"
              >
                <EyeIcon off={showNewPw} size={17} />
              </button>
            </div>
          </div>
          <button type="submit" className="neu-btn-primary" disabled={busy}>
            Ubah
          </button>
        </form>
      </section>

      <section className="neu-card">
        <div className="card-head">
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.45rem' }}>
            <SmartphoneIcon size={18} />
            <h2 className="card-title">Aplikasi Android</h2>
          </div>
          <span className={`badge ${statusData?.apk?.available ? 'badge-ok' : 'badge-muted'}`}>
            {statusData?.apk?.available ? 'Siap' : 'Belum'}
          </span>
        </div>
        <p className="helper">
          {statusData?.apk?.available && statusData.apk.size_bytes > 0
            ? `Android APK · ${(statusData.apk.size_bytes / (1024 * 1024)).toFixed(1)} MB`
            : 'Build APK via folder mobile'}
        </p>
        <button
          type="button"
          className="neu-btn-primary"
          disabled={apkDownloading}
          onClick={() => void handleDownloadAPK()}
          style={{ width: '100%' }}
        >
          <DownloadIcon size={16} />
          {apkDownloading ? 'Mengunduh...' : 'Download APK'}
        </button>
      </section>
    </div>
  );
}
