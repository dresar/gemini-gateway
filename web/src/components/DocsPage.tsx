'use client';

import { useState } from 'react';
import type { ProviderView } from '@/lib/types';
import { CopyIcon } from './Icons';

type AgentDocTab = 'terminal' | 'claude' | 'codex' | 'opencode' | 'gemini' | 'cli';

export function DocsPage({
  originURL,
  providers,
  sampleModel,
  onBack,
  onCopy,
}: {
  originURL: string;
  providers: ProviderView[];
  sampleModel: string;
  onBack: () => void;
  onCopy: (text: string, msg?: string) => void;
}) {
  const [agentTab, setAgentTab] = useState<AgentDocTab>('terminal');

  const categories = Array.from(new Set(providers.map((p) => p.category || 'gratis')));
  const firstCat = categories[0] || 'gratis';

  const snippets: Record<AgentDocTab, { title: string; cmd: string }> = {
    terminal: {
      title: 'Export Semua Agent (Bash / PowerShell)',
      cmd: `# Bash / Linux / macOS\nexport OPENAI_BASE_URL="${originURL}/v1"\nexport OPENAI_API_KEY="MASUKKAN_KEY_AKSES"\nexport ANTHROPIC_BASE_URL="${originURL}"\nexport ANTHROPIC_API_KEY="MASUKKAN_KEY_AKSES"\nexport GOOGLE_GEMINI_BASE_URL="${originURL}"\nexport GEMINI_API_KEY="MASUKKAN_KEY_AKSES"\n\n# PowerShell\n$env:OPENAI_BASE_URL="${originURL}/v1"\n$env:OPENAI_API_KEY="MASUKKAN_KEY_AKSES"\n$env:ANTHROPIC_BASE_URL="${originURL}"\n$env:ANTHROPIC_API_KEY="MASUKKAN_KEY_AKSES"\n$env:GOOGLE_GEMINI_BASE_URL="${originURL}"\n$env:GEMINI_API_KEY="MASUKKAN_KEY_AKSES"`,
    },
    claude: {
      title: 'Claude Code (/v1/messages)',
      cmd: `export ANTHROPIC_BASE_URL="${originURL}"\nexport ANTHROPIC_API_KEY="MASUKKAN_KEY_AKSES"\nclaude`,
    },
    codex: {
      title: 'Codex CLI (/v1/responses)',
      cmd: `export OPENAI_BASE_URL="${originURL}/v1"\nexport OPENAI_API_KEY="MASUKKAN_KEY_AKSES"\ncodex`,
    },
    opencode: {
      title: 'OpenCode / Aider / Cursor (/v1/chat/completions)',
      cmd: `export OPENAI_BASE_URL="${originURL}/v1"\nexport OPENAI_API_KEY="MASUKKAN_KEY_AKSES"\ncurl -s "${originURL}/v1/chat/completions" \\\n  -H "Authorization: Bearer $OPENAI_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{"model":"${sampleModel}","messages":[{"role":"user","content":"Hai"}]}'`,
    },
    gemini: {
      title: 'Gemini CLI (/v1beta/models)',
      cmd: `export GOOGLE_GEMINI_BASE_URL="${originURL}"\nexport GEMINI_API_KEY="MASUKKAN_KEY_AKSES"\ngemini`,
    },
    cli: {
      title: 'Eksekusi Cepat Terminal (geminigw CLI)',
      cmd: `# Cek env semua AI agent otomatis\n./geminigw agent-env --url "${originURL}" --key "MASUKKAN_KEY_AKSES"\n\n# Eksekusi prompt langsung di terminal (<1ms rotasi)\n./geminigw exec "Ringkas kode ini" --model auto\necho "Buatkan bash backup" | ./geminigw ask --provider ${providers[0]?.id || 'gemini'}`,
    },
  };

  const active = snippets[agentTab];

  return (
    <div className="list-stack">
      <section className="neu-card">
        <div className="card-head" style={{ marginBottom: 0 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.65rem' }}>
            <button type="button" className="neu-btn neu-btn-sm" onClick={onBack}>
              Kembali
            </button>
            <h1 className="card-title" style={{ fontSize: '1.05rem' }}>
              Panduan
            </h1>
          </div>
          <span className="badge badge-ok">Lengkap</span>
        </div>
      </section>

      <section className="neu-card">
        <div className="card-head">
          <h2 className="card-title">Alur Pakai</h2>
        </div>
        <div className="list-stack">
          <div className="neu-inset-box">
            <strong>1. Provider</strong>
            <div className="kpi-sub">
              Buka Pool → + Provider. Atur Nama, Kategori (mis. gratis, coding), Endpoint URL, Model, dan Ikon aset.
            </div>
          </div>
          <div className="neu-inset-box">
            <strong>2. Upstream Key</strong>
            <div className="kpi-sub">
              Masukkan 1 atau banyak API Key ke provider. Key dirotasi langsung dari RAM dalam mikrodetik.
            </div>
          </div>
          <div className="neu-inset-box">
            <strong>3. Key Akses & Kategori</strong>
            <div className="kpi-sub">
              Buka Akses → Buat. Pilih Global (semua provider), Grup ({`cat:${firstCat}`}), atau Provider khusus.
            </div>
          </div>
        </div>
      </section>

      <section className="neu-card">
        <div className="card-head">
          <h2 className="card-title">Terminal AI Agent</h2>
          <button
            type="button"
            className="neu-btn neu-btn-sm"
            onClick={() => onCopy(active.cmd, 'Perintah disalin')}
          >
            <CopyIcon size={14} />
            <span>Salin</span>
          </button>
        </div>

        <div className="pill-row" style={{ marginBottom: '0.75rem' }}>
          {(
            [
              ['terminal', 'Semua Env'],
              ['claude', 'Claude Code'],
              ['codex', 'Codex CLI'],
              ['opencode', 'OpenCode'],
              ['gemini', 'Gemini CLI'],
              ['cli', 'CLI Cepat'],
            ] as Array<[AgentDocTab, string]>
          ).map(([id, label]) => (
            <button
              key={id}
              type="button"
              className={`neu-btn neu-btn-sm ${agentTab === id ? 'is-active' : ''}`}
              onClick={() => setAgentTab(id)}
            >
              {label}
            </button>
          ))}
        </div>

        <div className="kpi-sub" style={{ marginBottom: '0.4rem', fontWeight: 600 }}>
          {active.title}
        </div>
        <pre className="code-box mono">{active.cmd}</pre>
      </section>

      <div className="settings-grid">
        <section className="neu-card">
          <div className="card-head">
            <h2 className="card-title">Status & 5x Gagal</h2>
          </div>
          <div className="list-stack">
            <div className="neu-inset-box" style={{ fontSize: '0.8125rem' }}>
              <strong>Gagal 1–4x → Cooldown</strong>
              <div className="kpi-sub">
                Rotasi langsung pindah ke key/provider lain. Kartu key mencatat jumlah gagal (1/5..4/5) dan alasan: Kredit habis (402), Key invalid (401/403), atau Rate limit (429).
              </div>
            </div>
            <div className="neu-inset-box" style={{ fontSize: '0.8125rem' }}>
              <strong>Gagal 5x → Mati (5/5)</strong>
              <div className="kpi-sub">
                Key otomatis ditandai Mati setelah 5 kali gagal berturut-turut dan dikeluarkan dari rotasi. Klik Pulih atau Bersih di Pool.
              </div>
            </div>
          </div>
        </section>

        <section className="neu-card">
          <div className="card-head">
            <h2 className="card-title">Endpoint Protokol</h2>
          </div>
          <div className="list-stack">
            <div className="neu-inset-box" style={{ fontSize: '0.8125rem' }}>
              <div className="mono">GET /v1/models?probe=1</div>
              <div className="kpi-sub">Daftar model lengkap (lokal atau live probe ke semua provider aktif)</div>
            </div>
            <div className="neu-inset-box" style={{ fontSize: '0.8125rem' }}>
              <div className="mono">POST /v1/chat/completions</div>
              <div className="kpi-sub">OpenAI SDK, OpenCode, Aider, Cursor, Cline, curl</div>
            </div>
            <div className="neu-inset-box" style={{ fontSize: '0.8125rem' }}>
              <div className="mono">POST /v1/responses</div>
              <div className="kpi-sub">Codex CLI & OpenAI Responses API</div>
            </div>
            <div className="neu-inset-box" style={{ fontSize: '0.8125rem' }}>
              <div className="mono">POST /v1/messages</div>
              <div className="kpi-sub">Claude Code (ANTHROPIC_BASE_URL)</div>
            </div>
            <div className="neu-inset-box" style={{ fontSize: '0.8125rem' }}>
              <div className="mono">POST /v1beta/models/*:generateContent</div>
              <div className="kpi-sub">Gemini CLI & Google GenAI SDK</div>
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}
