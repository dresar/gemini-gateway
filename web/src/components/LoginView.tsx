'use client';

import type React from 'react';
import { EyeIcon, KaRouterIcon } from './Icons';

export function LoginView({
  password,
  showPassword,
  error,
  busy,
  themeToggleBtn,
  onChangePassword,
  onToggleShowPassword,
  onSubmit,
}: {
  password: string;
  showPassword: boolean;
  error: string;
  busy: boolean;
  themeToggleBtn: React.ReactNode;
  onChangePassword: (next: string) => void;
  onToggleShowPassword: () => void;
  onSubmit: (e: React.FormEvent) => void;
}) {
  return (
    <div className="auth-screen">
      <div className="auth-card">
        <div className="auth-head">
          <div className="brand">
            <KaRouterIcon size={28} />
            <h1 className="brand-name">KaRouter</h1>
          </div>
          {themeToggleBtn}
        </div>
        <form onSubmit={onSubmit}>
          <div className="field">
            <label htmlFor="admin-password">Password</label>
            <div className="pw-wrap">
              <input
                id="admin-password"
                type={showPassword ? 'text' : 'password'}
                autoFocus
                required
                autoComplete="current-password"
                placeholder="••••••••"
                value={password}
                onChange={(e) => onChangePassword(e.target.value)}
                aria-invalid={error ? 'true' : undefined}
              />
              <button
                type="button"
                className="pw-eye"
                onClick={onToggleShowPassword}
                aria-label="Tampil password"
              >
                <EyeIcon off={showPassword} size={17} />
              </button>
            </div>
            {error ? (
              <span className="field-err" role="alert">
                {error}
              </span>
            ) : null}
          </div>
          <button type="submit" className="neu-btn-primary" style={{ width: '100%' }} disabled={busy}>
            {busy ? 'Proses...' : 'Masuk'}
          </button>
        </form>
      </div>
    </div>
  );
}
