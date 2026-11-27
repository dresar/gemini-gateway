'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { fetchUsage } from '@/lib/api';
import type { UsageModelSummary, UsageReport, UsageTotals } from '@/lib/types';

type Currency = 'IDR' | 'USD';

interface ModelRate {
  label: string;
  inputPerM: number;
  outputPerM: number;
  cacheReadPerM: number;
  cacheWritePerM: number;
}

const RANGE_OPTIONS: Array<{ days: number; label: string }> = [
  { days: 1, label: 'Hari Ini' },
  { days: 7, label: '7H' },
  { days: 30, label: '30H' },
  { days: 60, label: '60H' },
  { days: 180, label: '180H' },
  { days: 365, label: '1 Tahun' },
];

const DEFAULT_USD_IDR = 16300;

function detectModelRate(model: string): ModelRate {
  const m = (model || '').toLowerCase();
  if (m.includes('flash-lite') || m.includes('nano') || m.includes('mini')) {
    return { label: 'Flash Lite', inputPerM: 0.1, outputPerM: 0.4, cacheReadPerM: 0.025, cacheWritePerM: 0.1 };
  }
  if (m.includes('flash') || m.includes('agnes')) {
    return { label: 'Flash', inputPerM: 0.3, outputPerM: 2.5, cacheReadPerM: 0.075, cacheWritePerM: 0.3 };
  }
  if (m.includes('hydrogen') || m.includes('pro')) {
    return { label: 'Pro / Hydrogen', inputPerM: 1.25, outputPerM: 10.0, cacheReadPerM: 0.3125, cacheWritePerM: 1.25 };
  }
  if (m.includes('opus')) {
    return { label: 'Opus', inputPerM: 15.0, outputPerM: 75.0, cacheReadPerM: 1.5, cacheWritePerM: 18.75 };
  }
  if (m.includes('sonnet')) {
    return { label: 'Sonnet', inputPerM: 3.0, outputPerM: 15.0, cacheReadPerM: 0.3, cacheWritePerM: 3.75 };
  }
  if (m.includes('haiku')) {
    return { label: 'Haiku', inputPerM: 0.8, outputPerM: 4.0, cacheReadPerM: 0.08, cacheWritePerM: 1.0 };
  }
  if (m.includes('deepseek')) {
    return { label: 'DeepSeek', inputPerM: 0.27, outputPerM: 1.1, cacheReadPerM: 0.07, cacheWritePerM: 0.27 };
  }
  if (m.includes('gpt-4o') || m.includes('gpt-5')) {
    return { label: 'GPT', inputPerM: 2.5, outputPerM: 10.0, cacheReadPerM: 1.25, cacheWritePerM: 2.5 };
  }
  if (m.includes('glm')) {
    return { label: 'GLM', inputPerM: 0.2, outputPerM: 0.8, cacheReadPerM: 0.05, cacheWritePerM: 0.2 };
  }
  return { label: 'Standar', inputPerM: 0.3, outputPerM: 2.5, cacheReadPerM: 0.075, cacheWritePerM: 0.3 };
}

function calcCostUSD(row: UsageTotals, rate: ModelRate) {
  const inputUSD = (row.input_tokens / 1_000_000) * rate.inputPerM;
  const outputUSD = ((row.output_tokens + row.reasoning_tokens) / 1_000_000) * rate.outputPerM;
  const cacheUSD =
    (row.cache_read_tokens / 1_000_000) * rate.cacheReadPerM +
    (row.cache_write_tokens / 1_000_000) * rate.cacheWritePerM;
  return {
    inputUSD,
    outputUSD,
    cacheUSD,
    totalUSD: inputUSD + outputUSD + cacheUSD,
  };
}

function fmtMillions(tokens: number): string {
  const m = tokens / 1_000_000;
  if (m >= 100) return `${m.toFixed(1)}M`;
  if (m >= 1) return `${m.toFixed(2)}M`;
  if (m >= 0.01) return `${m.toFixed(3)}M`;
  return `${m.toFixed(4)}M`;
}

function fmtCompactInt(n: number): string {
  return Math.round(n || 0).toLocaleString('id-ID');
}

function fmtMoney(usd: number, currency: Currency, usdIdrRate: number): string {
  if (currency === 'IDR') {
    const idr = usd * usdIdrRate;
    if (idr > 0 && idr < 1) {
      return `Rp ${idr.toFixed(2)}`;
    }
    return `Rp ${Math.round(idr).toLocaleString('id-ID')}`;
  }
  if (usd > 0 && usd < 0.01) {
    return `$${usd.toFixed(4)}`;
  }
  return `$${usd.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

export function UsageCostCard({
  initialUsage,
  onSessionExpired,
}: {
  initialUsage?: UsageReport;
  onSessionExpired: () => Promise<void>;
}) {
  const [days, setDays] = useState<number>(60);
  const [currency, setCurrency] = useState<Currency>('IDR');
  const [usdIdrRate, setUsdIdrRate] = useState<number>(DEFAULT_USD_IDR);
  const [report, setReport] = useState<UsageReport | null>(initialUsage || null);
  const [lastTick, setLastTick] = useState<number>(0);

  useEffect(() => {
    try {
      const savedCurr = window.localStorage.getItem('karouter_currency');
      if (savedCurr === 'USD' || savedCurr === 'IDR') setCurrency(savedCurr);
      const savedRate = Number(window.localStorage.getItem('karouter_usd_idr'));
      if (Number.isFinite(savedRate) && savedRate > 1000) setUsdIdrRate(savedRate);
    } catch {
    }
  }, []);

  useEffect(() => {
    if (days === 60 && initialUsage) {
      setReport(initialUsage);
      setLastTick(Date.now());
    }
  }, [days, initialUsage]);

  const loadUsage = useCallback(async () => {
    try {
      const res = await fetchUsage(days);
      setReport(res);
      setLastTick(Date.now());
    } catch (err) {
      const msg = err instanceof Error ? err.message : '';
      if (msg.toLowerCase().includes('unauthorized') || msg.includes('401')) {
        await onSessionExpired();
      }
    }
  }, [days, onSessionExpired]);

  useEffect(() => {
    void loadUsage();
    const id = setInterval(() => {
      void loadUsage();
    }, 2000);
    return () => clearInterval(id);
  }, [loadUsage]);

  const selectCurrency = (next: Currency) => {
    setCurrency(next);
    try {
      window.localStorage.setItem('karouter_currency', next);
    } catch {
    }
  };

  const updateRate = (raw: string) => {
    const val = Number(raw);
    if (!Number.isFinite(val) || val <= 0) return;
    setUsdIdrRate(val);
    try {
      window.localStorage.setItem('karouter_usd_idr', String(val));
    } catch {
    }
  };

  const totals: UsageTotals = report?.totals || {
    requests: 0,
    input_tokens: 0,
    output_tokens: 0,
    cache_read_tokens: 0,
    cache_write_tokens: 0,
    reasoning_tokens: 0,
    total_tokens: 0,
  };

  const byModelWithCost = useMemo(() => {
    const list = report?.by_model || [];
    return list.map((item: UsageModelSummary) => {
      const rate = detectModelRate(item.model);
      const cost = calcCostUSD(item, rate);
      return { ...item, rate, cost };
    });
  }, [report?.by_model]);

  const totalCost = useMemo(() => {
    if (byModelWithCost.length === 0) {
      return calcCostUSD(totals, detectModelRate('flash'));
    }
    return byModelWithCost.reduce(
      (acc, row) => ({
        inputUSD: acc.inputUSD + row.cost.inputUSD,
        outputUSD: acc.outputUSD + row.cost.outputUSD,
        cacheUSD: acc.cacheUSD + row.cost.cacheUSD,
        totalUSD: acc.totalUSD + row.cost.totalUSD,
      }),
      { inputUSD: 0, outputUSD: 0, cacheUSD: 0, totalUSD: 0 }
    );
  }, [byModelWithCost, totals]);

  const cacheTotal = totals.cache_read_tokens + totals.cache_write_tokens;
  const updatedText = lastTick
    ? new Date(lastTick).toLocaleTimeString('id-ID', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
    : '--:--:--';

  return (
    <section className="neu-card" style={{ gridColumn: '1 / -1' }}>
      <div className="card-head" style={{ flexWrap: 'wrap', gap: '0.6rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.55rem', flexWrap: 'wrap' }}>
          <h2 className="card-title">Penggunaan Token &amp; Simulasi Biaya</h2>
          <span className="badge badge-ok" title={`Diperbarui otomatis setiap 2 detik (${updatedText})`}>
            LIVE · {updatedText}
          </span>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
          <div className="pill-row" style={{ margin: 0 }}>
            <button
              type="button"
              className={`neu-btn neu-btn-sm ${currency === 'IDR' ? 'is-active' : ''}`}
              onClick={() => selectCurrency('IDR')}
            >
              IDR (Rp)
            </button>
            <button
              type="button"
              className={`neu-btn neu-btn-sm ${currency === 'USD' ? 'is-active' : ''}`}
              onClick={() => selectCurrency('USD')}
            >
              USD ($)
            </button>
          </div>
          {currency === 'IDR' ? (
            <label style={{ display: 'flex', alignItems: 'center', gap: '0.35rem', fontSize: '0.78rem' }}>
              <span>Kurs:</span>
              <input
                type="number"
                min={1000}
                step={100}
                value={usdIdrRate}
                onChange={(e) => updateRate(e.target.value)}
                style={{ width: '5.5rem', padding: '0.28rem 0.45rem', fontSize: '0.78rem' }}
                aria-label="Kurs USD ke IDR"
              />
            </label>
          ) : null}
        </div>
      </div>

      <div className="pill-row" style={{ marginBottom: '0.85rem' }}>
        {RANGE_OPTIONS.map((opt) => (
          <button
            key={opt.days}
            type="button"
            className={`neu-btn neu-btn-sm ${days === opt.days ? 'is-active' : ''}`}
            onClick={() => setDays(opt.days)}
          >
            {opt.label}
          </button>
        ))}
      </div>

      <div className="status-meta-grid" style={{ marginBottom: '0.85rem' }}>
        <div className="status-meta-cell">
          <div className="status-meta-label">Total Token ({days}H)</div>
          <div className="status-meta-val mono">{fmtMillions(totals.total_tokens)}</div>
          <div className="kpi-sub mono">
            {fmtCompactInt(totals.total_tokens)} tok · {fmtCompactInt(totals.requests)} req
          </div>
        </div>

        <div className="status-meta-cell">
          <div className="status-meta-label">Simulasi Biaya ({currency})</div>
          <div className="status-meta-val mono" style={{ color: 'var(--accent)' }}>
            {fmtMoney(totalCost.totalUSD, currency, usdIdrRate)}
          </div>
          <div className="kpi-sub mono">
            {currency === 'IDR'
              ? `${fmtMoney(totalCost.totalUSD, 'USD', usdIdrRate)} @ Rp ${usdIdrRate.toLocaleString('id-ID')}`
              : `${fmtMoney(totalCost.totalUSD, 'IDR', usdIdrRate)}`}
          </div>
        </div>

        <div className="status-meta-cell">
          <div className="status-meta-label">Input Token</div>
          <div className="status-meta-val mono">{fmtMillions(totals.input_tokens)}</div>
          <div className="kpi-sub mono">
            {fmtCompactInt(totals.input_tokens)} · {fmtMoney(totalCost.inputUSD, currency, usdIdrRate)}
          </div>
        </div>

        <div className="status-meta-cell">
          <div className="status-meta-label">Output Token</div>
          <div className="status-meta-val mono">{fmtMillions(totals.output_tokens)}</div>
          <div className="kpi-sub mono">
            {fmtCompactInt(totals.output_tokens)} · {fmtMoney(totalCost.outputUSD, currency, usdIdrRate)}
          </div>
        </div>

        <div className="status-meta-cell">
          <div className="status-meta-label">Cache (Read + Write)</div>
          <div className="status-meta-val mono">{fmtMillions(cacheTotal)}</div>
          <div className="kpi-sub mono">
            R:{fmtCompactInt(totals.cache_read_tokens)} W:{fmtCompactInt(totals.cache_write_tokens)} ·{' '}
            {fmtMoney(totalCost.cacheUSD, currency, usdIdrRate)}
          </div>
        </div>

        <div className="status-meta-cell">
          <div className="status-meta-label">Reasoning / Thought</div>
          <div className="status-meta-val mono">{fmtMillions(totals.reasoning_tokens)}</div>
          <div className="kpi-sub mono">{fmtCompactInt(totals.reasoning_tokens)} tok</div>
        </div>
      </div>

      {byModelWithCost.length > 0 ? (
        <details className="acc-details" open>
          <summary>Rincian per Model ({byModelWithCost.length} model · disimpan SQLite s.d. 365 hari)</summary>
          <div className="acc-body">
            <div className="table-scroll">
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.8rem' }}>
                <thead>
                  <tr style={{ textAlign: 'left', borderBottom: '1px solid var(--border)' }}>
                    <th style={{ padding: '0.4rem' }}>Model</th>
                    <th style={{ padding: '0.4rem' }}>Req</th>
                    <th style={{ padding: '0.4rem' }}>Total (M)</th>
                    <th style={{ padding: '0.4rem' }}>Input</th>
                    <th style={{ padding: '0.4rem' }}>Output</th>
                    <th style={{ padding: '0.4rem' }}>Cache</th>
                    <th style={{ padding: '0.4rem' }}>Simulasi ({currency})</th>
                  </tr>
                </thead>
                <tbody>
                  {byModelWithCost.map((m) => (
                    <tr key={`${m.provider}:${m.model}`} style={{ borderBottom: '1px solid var(--border)' }}>
                      <td style={{ padding: '0.4rem' }}>
                        <div className="mono" style={{ fontWeight: 600 }}>{m.model}</div>
                        <div className="kpi-sub">
                          {m.provider} · {m.rate.label} (${m.rate.inputPerM}/${m.rate.outputPerM}/1M)
                        </div>
                      </td>
                      <td style={{ padding: '0.4rem' }} className="mono">{fmtCompactInt(m.requests)}</td>
                      <td style={{ padding: '0.4rem' }} className="mono">{fmtMillions(m.total_tokens)}</td>
                      <td style={{ padding: '0.4rem' }} className="mono">{fmtCompactInt(m.input_tokens)}</td>
                      <td style={{ padding: '0.4rem' }} className="mono">
                        {fmtCompactInt(m.output_tokens)}
                        {m.reasoning_tokens > 0 ? ` (+${fmtCompactInt(m.reasoning_tokens)})` : ''}
                      </td>
                      <td style={{ padding: '0.4rem' }} className="mono">
                        {fmtCompactInt(m.cache_read_tokens + m.cache_write_tokens)}
                      </td>
                      <td style={{ padding: '0.4rem', fontWeight: 600 }} className="mono">
                        {fmtMoney(m.cost.totalUSD, currency, usdIdrRate)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </details>
      ) : (
        <div className="neu-inset-box" style={{ fontSize: '0.8125rem' }}>
          Belum ada data token pada rentang {days} hari ini. Data akan muncul otomatis secara real-time saat request masuk.
        </div>
      )}

      {(report?.daily?.length || 0) > 0 ? (
        <details className="acc-details" style={{ marginTop: '0.65rem' }}>
          <summary>Riwayat Harian SQLite ({report?.daily.length || 0} hari tercatat)</summary>
          <div className="acc-body">
            <div className="table-scroll" style={{ maxHeight: '240px', overflowY: 'auto' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.78rem' }}>
                <thead>
                  <tr style={{ textAlign: 'left', borderBottom: '1px solid var(--border)' }}>
                    <th style={{ padding: '0.35rem' }}>Tanggal</th>
                    <th style={{ padding: '0.35rem' }}>Req</th>
                    <th style={{ padding: '0.35rem' }}>Total (M)</th>
                    <th style={{ padding: '0.35rem' }}>Input</th>
                    <th style={{ padding: '0.35rem' }}>Output</th>
                    <th style={{ padding: '0.35rem' }}>Cache</th>
                    <th style={{ padding: '0.35rem' }}>Thought</th>
                  </tr>
                </thead>
                <tbody>
                  {(report?.daily || []).map((d) => (
                    <tr key={d.day} style={{ borderBottom: '1px solid var(--border)' }}>
                      <td style={{ padding: '0.35rem' }} className="mono">{d.day}</td>
                      <td style={{ padding: '0.35rem' }} className="mono">{fmtCompactInt(d.requests)}</td>
                      <td style={{ padding: '0.35rem' }} className="mono">{fmtMillions(d.total_tokens)}</td>
                      <td style={{ padding: '0.35rem' }} className="mono">{fmtCompactInt(d.input_tokens)}</td>
                      <td style={{ padding: '0.35rem' }} className="mono">{fmtCompactInt(d.output_tokens)}</td>
                      <td style={{ padding: '0.35rem' }} className="mono">
                        {fmtCompactInt(d.cache_read_tokens + d.cache_write_tokens)}
                      </td>
                      <td style={{ padding: '0.35rem' }} className="mono">{fmtCompactInt(d.reasoning_tokens)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </details>
      ) : null}
    </section>
  );
}
