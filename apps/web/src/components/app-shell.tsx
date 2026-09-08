'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import type { ReactNode } from 'react';

import { ConnectButton } from '@/components/connect-button';
import { NotificationBell } from '@/components/notification-bell';
import { env, isAuthConfigured } from '@/lib/env';

const navigation = [
  { href: '/', label: 'Overview' },
  { href: '/sessions', label: 'Sessions' },
  { href: '/media', label: 'Media' },
  { href: '/onchain', label: 'Onchain' },
] as const;

export function AppShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();

  return (
    <div className="flex min-h-screen flex-col">
      <header className="border-b border-[var(--color-border)] bg-[var(--color-surface)]">
        <div className="mx-auto flex h-14 w-full max-w-6xl items-center gap-6 px-4">
          <Link href="/" className="text-sm font-semibold tracking-tight">
            {env.NEXT_PUBLIC_APP_NAME}
          </Link>

          <nav className="flex items-center gap-1" aria-label="Main">
            {navigation.map((item) => {
              // Every route starts with "/", so the root needs an exact match
              // or it would highlight on every page.
              const isActive =
                item.href === '/' ? pathname === '/' : pathname.startsWith(item.href);

              return (
                <Link
                  key={item.href}
                  href={item.href}
                  aria-current={isActive ? 'page' : undefined}
                  className={`rounded-md px-3 py-1.5 text-sm transition-colors ${
                    isActive
                      ? 'bg-[var(--color-surface-muted)] font-medium text-[var(--color-ink)]'
                      : 'text-[var(--color-ink-muted)] hover:text-[var(--color-ink)]'
                  }`}
                >
                  {item.label}
                </Link>
              );
            })}
          </nav>

          <div className="ml-auto flex items-center gap-3">
            <NotificationBell />
            <ConnectButton />
          </div>
        </div>
      </header>

      {!isAuthConfigured && <ConfigurationNotice />}

      <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-8">{children}</main>

      <footer className="border-t border-[var(--color-border)] px-4 py-4">
        <div className="mx-auto max-w-6xl text-xs text-[var(--color-ink-muted)]">
          {env.NEXT_PUBLIC_APP_NAME} · API {env.NEXT_PUBLIC_API_URL}
        </div>
      </footer>
    </div>
  );
}

/**
 * Without a Dynamic environment id there is no sign-in at all. Say so plainly
 * instead of rendering a connect button that cannot work.
 */
function ConfigurationNotice() {
  return (
    <div className="border-b border-[var(--color-warning)]/40 bg-[var(--color-warning)]/10 px-4 py-2">
      <p className="mx-auto max-w-6xl text-xs text-[var(--color-ink)]">
        Wallet authentication is disabled: set{' '}
        <code className="font-mono">NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID</code> in{' '}
        <code className="font-mono">apps/web/.env.local</code> to enable sign-in.
      </p>
    </div>
  );
}
