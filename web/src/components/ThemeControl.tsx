'use client';

import { useEffect, useState } from 'react';
import type { ThemeChoice } from '@/lib/types';
import { MonitorIcon, MoonIcon, SunIcon } from './Icons';

function readStoredScheme(): ThemeChoice {
  try {
    const val = localStorage.getItem('color-scheme');
    if (val === 'light' || val === 'dark') return val;
  } catch {
  }
  return 'system';
}

function getSystemIsDark(): boolean {
  if (typeof window === 'undefined') return false;
  return window.matchMedia('(prefers-color-scheme: dark)').matches;
}

function syncMetaScheme(choice: ThemeChoice) {
  const meta = document.querySelector('meta[name="color-scheme"]');
  if (meta) {
    meta.setAttribute('content', choice === 'system' ? 'light dark' : choice);
  }
}

export function useColorSchemeSync() {
  const [choice, setChoice] = useState<ThemeChoice>('system');
  const [systemDark, setSystemDark] = useState(false);

  useEffect(() => {
    const initial = readStoredScheme();
    setChoice(initial);
    setSystemDark(getSystemIsDark());
    syncMetaScheme(initial);

    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    const onMqChange = (e: MediaQueryListEvent) => setSystemDark(e.matches);
    const onStorage = (e: StorageEvent) => {
      if (e.key === 'color-scheme') {
        const next = readStoredScheme();
        setChoice(next);
        syncMetaScheme(next);
      }
    };
    mq.addEventListener('change', onMqChange);
    window.addEventListener('storage', onStorage);
    return () => {
      mq.removeEventListener('change', onMqChange);
      window.removeEventListener('storage', onStorage);
    };
  }, []);

  const renderedDark = choice === 'dark' || (choice === 'system' && systemDark);

  const applyChoice = (next: ThemeChoice) => {
    setChoice(next);
    syncMetaScheme(next);
    try {
      if (next === 'system') {
        localStorage.removeItem('color-scheme');
      } else {
        localStorage.setItem('color-scheme', next);
      }
    } catch {
    }
  };

  const toggleTwoState = () => {
    const target: 'light' | 'dark' = renderedDark ? 'light' : 'dark';
    const sysPref: 'light' | 'dark' = systemDark ? 'dark' : 'light';
    if (target === sysPref) {
      applyChoice('system');
    } else {
      applyChoice(target);
    }
  };

  return { choice, systemDark, renderedDark, applyChoice, toggleTwoState };
}

export function HeaderThemeToggle({
  renderedDark,
  onToggle,
}: {
  renderedDark: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      className="neu-icon-btn"
      onClick={onToggle}
      aria-label={renderedDark ? 'Mode terang' : 'Mode gelap'}
      title="Tema"
    >
      <span className="theme-icon-light" aria-hidden="true">
        <MoonIcon size={18} />
      </span>
      <span className="theme-icon-dark" aria-hidden="true">
        <SunIcon size={18} />
      </span>
    </button>
  );
}

export function SettingsThemeSelector({
  choice,
  onChange,
}: {
  choice: ThemeChoice;
  systemDark?: boolean;
  onChange: (next: ThemeChoice) => void;
}) {
  const options: { value: ThemeChoice; label: string }[] = [
    { value: 'system', label: 'Sistem' },
    { value: 'light', label: 'Terang' },
    { value: 'dark', label: 'Gelap' },
  ];

  return (
    <div className="field" style={{ marginBottom: 0 }}>
      <label>Tema</label>
      <div className="seg-grid-3">
        {options.map((opt) => (
          <label
            key={opt.value}
            className={`seg-option ${choice === opt.value ? 'is-selected' : ''}`}
          >
            <input
              type="radio"
              name="theme_choice"
              value={opt.value}
              checked={choice === opt.value}
              onChange={() => onChange(opt.value)}
            />
            {opt.value === 'system' ? (
              <MonitorIcon size={16} />
            ) : opt.value === 'light' ? (
              <SunIcon size={16} />
            ) : (
              <MoonIcon size={16} />
            )}
            <span>{opt.label}</span>
          </label>
        ))}
      </div>
    </div>
  );
}
