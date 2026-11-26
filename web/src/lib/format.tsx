import { AlertIcon, CheckCircleIcon, ClockIcon, PauseIcon } from '@/components/Icons';
import type { KeyView, ThemeChoice } from './types';

export type TabId = 'home' | 'akses' | 'pool' | 'uji' | 'atur';
export type PageView = null | 'provider-form' | 'docs';
export type StatusFilter = 'all' | 'healthy' | 'cooldown' | 'dead' | 'paused';
export type LogFilter = 'all' | 'error' | 'ok';

export interface ToastState {
  id: number;
  message: string;
  tone: 'ok' | 'err';
}

export interface PendingConfirm {
  title: string;
  message: string;
  confirmLabel: string;
  run: () => Promise<void>;
}

export const THEME_STORAGE_KEY = 'karouter_theme';
export const BAR_PALETTE = ['#356df3', '#0d8a68', '#7c5cf2', '#d97706', '#db2777', '#0891b2'];
const TOAST_MAX_CHARS = 72;

export function clipToast(raw: string): string {
  const flat = String(raw || '').replace(/[\r\n\t]+/g, ' ').replace(/\s{2,}/g, ' ').trim();
  if (!flat) return 'Selesai';
  if (flat.length <= TOAST_MAX_CHARS) return flat;
  return flat.slice(0, TOAST_MAX_CHARS - 1).trimEnd() + '…';
}

export function errorCodeLabel(code?: string): string {
  switch (code) {
    case 'credit_exhausted':
      return 'Kredit habis';
    case 'invalid_key':
      return 'Key invalid';
    case 'rate_limit':
      return 'Rate limit';
    case 'model_error':
      return 'Model error';
    case 'network_error':
      return 'Timeout';
    case 'pool_empty':
      return 'Pool kosong';
    case 'upstream_error':
      return 'Upstream';
    default:
      return code ? code : '';
  }
}

export function applyThemeChoice(choice: ThemeChoice) {
  const meta = document.querySelector('meta[name="color-scheme"]');
  if (meta) {
    meta.setAttribute('content', choice === 'system' ? 'light dark' : choice);
  }
  try {
    localStorage.setItem(THEME_STORAGE_KEY, choice);
  } catch {
  }
}

export function readStoredTheme(): ThemeChoice {
  try {
    const raw = localStorage.getItem(THEME_STORAGE_KEY);
    if (raw === 'light' || raw === 'dark' || raw === 'system') return raw;
  } catch {
  }
  return 'system';
}

export function effectiveIsDark(choice: ThemeChoice): boolean {
  if (choice === 'dark') return true;
  if (choice === 'light') return false;
  if (typeof window !== 'undefined' && window.matchMedia) {
    return window.matchMedia('(prefers-color-scheme: dark)').matches;
  }
  return false;
}

export function fmtTime(unixSec: number): string {
  if (!unixSec) return '—';
  const d = new Date(unixSec * 1000);
  return d.toLocaleTimeString('id-ID', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

export function fmtDateTime(unixSec: number): string {
  if (!unixSec) return '—';
  const d = new Date(unixSec * 1000);
  return d.toLocaleString('id-ID', {
    day: '2-digit',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function fmtUptime(sec?: number): string {
  const s = Math.max(0, Math.floor(sec || 0));
  if (s < 60) return `${s}d`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  const remM = m % 60;
  if (h < 24) return `${h}j ${remM}m`;
  const d = Math.floor(h / 24);
  return `${d}h ${h % 24}j`;
}

export function fmtSessionRemain(expiresAt: number | null): string {
  if (!expiresAt) return '24j';
  const diff = Math.max(0, expiresAt - Math.floor(Date.now() / 1000));
  const h = Math.floor(diff / 3600);
  const m = Math.floor((diff % 3600) / 60);
  return `${h}j ${m}m`;
}

export function statusBadgeMeta(k: KeyView): {
  label: string;
  cls: string;
  kind: 'ok' | 'warn' | 'err' | 'muted';
} {
  if (!k.enabled || k.status === 'paused') {
    return { label: 'Jeda', cls: 'badge badge-muted', kind: 'muted' };
  }
  if (k.status === 'dead') {
    return {
      label: k.consec_fails >= 5 ? `Mati (${k.consec_fails}/5)` : 'Mati',
      cls: 'badge badge-err',
      kind: 'err',
    };
  }
  if (k.status === 'cooldown') {
    return {
      label:
        k.consec_fails > 0
          ? `Gagal ${k.consec_fails}/5 · ${Math.max(1, k.cooldown_remaining_sec)}d`
          : `${Math.max(1, k.cooldown_remaining_sec)}d`,
      cls: 'badge badge-warn',
      kind: 'warn',
    };
  }
  if (k.consec_fails > 0) {
    return {
      label: `Gagal ${k.consec_fails}/5`,
      cls: 'badge badge-warn',
      kind: 'warn',
    };
  }
  return { label: 'Siap', cls: 'badge badge-ok', kind: 'ok' };
}

export function StatusGlyph({ kind }: { kind: 'ok' | 'warn' | 'err' | 'muted' }) {
  if (kind === 'ok') return <CheckCircleIcon size={13} />;
  if (kind === 'warn') return <ClockIcon size={13} />;
  if (kind === 'err') return <AlertIcon size={13} />;
  return <PauseIcon size={13} />;
}
