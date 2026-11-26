# Frontend Next.js (`web/src/`, Static Export Single-File)

UI dibangun dengan Next.js static export dan dibundel menjadi 1 file [../../cmd/ui/index.html](../../cmd/ui/index.html) (`npm run build` di `web/`). Semua modul dijaga < 750 baris:

- `app/layout.tsx`, `app/tokens.css`, `app/layout_shell.css`, `app/ui_components.css`, `app/globals.css`: root layout & stylesheet Neumorphism Light/Dark terpisah.
- `app/page.tsx`: root state orchestrator, auth/JWT 24h auto-logout, header, bottom nav, dan switcher tab/halaman.
- `lib/types.ts`, `lib/api.ts`, `lib/format.tsx`: TypeScript interfaces, HTTP fetch client, helper format waktu & badge status.
- `components/LoginView.tsx`, `components/HomeDashboardTab.tsx`, `components/AksesTab.tsx`, `components/PoolTab.tsx`, `components/UjiLogsTab.tsx`, `components/AturTab.tsx`: tampilan per tab utama.
- `components/AccessGuideAndModels.tsx`, `components/DocsPage.tsx`, `components/ProviderFormPage.tsx`, `components/KeyModal.tsx`, `components/Icons.tsx`, `components/ProviderIcons.tsx`, `components/ThemeControl.tsx`: fitur spesialisasi & komponen UI.
