import './global.css';
import { RootProvider } from 'fumadocs-ui/provider/next';
import type { ReactNode } from 'react';
import type { Metadata } from 'next';

export const metadata: Metadata = {
  metadataBase: new URL('https://leroymerlin-cli.example'),
  title: { default: 'leroymerlin-cli', template: '%s | leroymerlin-cli' },
  description:
    'An unofficial, read-only terminal client for leroymerlin.es: stores, categories, products and stock as JSON.',
};

export default function Layout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className="flex min-h-screen flex-col">
        <RootProvider theme={{ defaultTheme: 'light' }} search={{ options: { type: 'static' } }}>
          {children}
        </RootProvider>
      </body>
    </html>
  );
}
