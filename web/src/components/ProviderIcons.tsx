import type { ReactNode } from 'react';
import { ICON_DATA_URIS } from '@/lib/iconAssets';

export interface ProviderIconMeta {
  id: string;
  label: string;
  assetPath: string;
}

export const PROVIDER_ICON_OPTIONS: ProviderIconMeta[] = [
  { id: 'gemini', label: 'Gemini', assetPath: '/assets/icons/gemini.svg' },
  { id: 'agnes', label: 'Agnes', assetPath: '/assets/icons/agnes.png' },
  { id: 'glm', label: 'GLM', assetPath: '/assets/icons/glm.svg' },
  { id: 'groq', label: 'Groq', assetPath: '/assets/icons/groq.svg' },
  { id: 'openrouter', label: 'OpenRouter', assetPath: '/assets/icons/openrouter.svg' },
  { id: 'mistral', label: 'Mistral', assetPath: '/assets/icons/mistral.svg' },
  { id: 'qwen', label: 'Qwen', assetPath: '/assets/icons/qwen.svg' },
  { id: 'huggingface', label: 'HF', assetPath: '/assets/icons/huggingface.svg' },
  { id: 'cloudflare', label: 'Cloudflare', assetPath: '/assets/icons/cloudflare.svg' },
  { id: 'nvidia', label: 'NVIDIA', assetPath: '/assets/icons/nvidia.svg' },
  { id: 'cohere', label: 'Cohere', assetPath: '/assets/icons/cohere.svg' },
  { id: 'ollama', label: 'Ollama', assetPath: '/assets/icons/ollama.svg' },
  { id: 'globe', label: 'Web', assetPath: '/assets/icons/globe.svg' },
  { id: 'custom', label: 'Custom', assetPath: '/assets/icons/custom.svg' },
];

const WIDE_ICONS = new Set(['groq', 'cloudflare', 'nvidia', 'cohere']);
const LIGHT_PLATE_ICONS = new Set(['openrouter', 'ollama', 'nvidia', 'cloudflare', 'cohere']);

export function ProviderGlyph({ icon, size = 18 }: { icon?: string; size?: number }): ReactNode {
  const key = (icon || 'custom').trim();
  const isRemote =
    key.startsWith('http://') ||
    key.startsWith('https://') ||
    key.startsWith('data:image/') ||
    key.startsWith('/assets/');
  const src = isRemote ? key : ICON_DATA_URIS[key] || ICON_DATA_URIS.custom;
  const isWide = WIDE_ICONS.has(key);
  const needsLightPlate = LIGHT_PLATE_ICONS.has(key);

  if (src) {
    return (
      <span
        className={`prov-glyph ${needsLightPlate ? 'is-light-plate' : ''}`}
        style={{ height: needsLightPlate ? size + 6 : size }}
      >
        <img
          src={src}
          alt=""
          aria-hidden="true"
          width={isWide ? Math.round(size * 2.4) : size}
          height={size}
          style={{
            height: size,
            width: 'auto',
            maxWidth: isWide ? Math.round(size * 2.75) : size,
            objectFit: 'contain',
            display: 'block',
            borderRadius: key === 'agnes' ? 5 : 2,
          }}
        />
      </span>
    );
  }

  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <rect x="4" y="4" width="16" height="16" rx="4" />
      <circle cx="12" cy="12" r="2.5" />
    </svg>
  );
}
