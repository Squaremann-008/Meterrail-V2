'use client';

import { DynamicWidget } from '@dynamic-labs/sdk-react-core';

import { useMe } from '@/hooks/use-api';
import { isAuthConfigured } from '@/lib/env';

/**
 * The Dynamic widget handles the whole wallet flow — connect, signature
 * challenge, embedded wallets, account switching — so there is no custom
 * connect UI to maintain here.
 */
export function ConnectButton() {
  const { data: user } = useMe();

  if (!isAuthConfigured) {
    return <span className="text-xs text-[var(--color-ink-muted)]">auth disabled</span>;
  }

  return (
    <div className="flex items-center gap-3">
      {user && (
        <span className="hidden text-xs text-[var(--color-ink-muted)] sm:inline">
          {user.displayName}
        </span>
      )}
      <DynamicWidget />
    </div>
  );
}
