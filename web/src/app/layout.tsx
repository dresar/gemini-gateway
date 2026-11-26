import type { Metadata } from 'next';
import './globals.css';

export const metadata: Metadata = {
  title: 'KaRouter',
  description: 'KaRouter Multi-Provider AI Gateway',
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="id" suppressHydrationWarning>
      <head>
        <meta name="color-scheme" content="light dark" />
        <script
          dangerouslySetInnerHTML={{
            __html: `try{var m=document.querySelector('meta[name="color-scheme"]');var v=localStorage.getItem("color-scheme");if(m)m.content=(v==="light"||v==="dark")?v:"light dark";}catch(e){}`,
          }}
        />
      </head>
      <body>{children}</body>
    </html>
  );
}
