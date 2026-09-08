'use client';

import { DynamicContextProvider, getAuthToken } from '@dynamic-labs/sdk-react-core';
import { EthereumWalletConnectors } from '@dynamic-labs/ethereum';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useState, type ReactNode } from 'react';

import { DynamicAuthBridge, UnauthenticatedProvider } from '@/components/auth-context';
import { ApiError, setAuthTokenGetter } from '@/lib/api-client';
import { env, isAuthConfigured } from '@/lib/env';

/**
 * Bridge the Dynamic SDK's token into the API client. Dynamic owns refresh, so
 * we read the current token per request instead of holding a copy.
 *
 * The read is guarded because the SDK throws when its store has not been
 * initialised, which is the case during prerendering and whenever the app runs
 * without a Dynamic environment id.
 */
setAuthTokenGetter(() => {
  try {
    return getAuthToken();
  } catch {
    return null;
  }
});

function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        gcTime: 5 * 60_000,
        refetchOnWindowFocus: false,
        retry: (failureCount, error) => {
          // Re-authenticating fixes a 401; retrying does not. Client errors
          // are equally pointless to repeat.
          if (error instanceof ApiError && !error.isRetryable) return false;
          return failureCount < 2;
        },
      },
      mutations: {
        retry: false,
      },
    },
  });
}

export function Providers({ children }: { children: ReactNode }) {
  // A factory in useState keeps one client per browser tab and stops React
  // Strict Mode's double-render from discarding the cache.
  const [queryClient] = useState(createQueryClient);

  // Without a Dynamic environment id the SDK cannot mount at all, so render
  // the app unauthenticated rather than crashing the whole tree.
  if (!isAuthConfigured) {
    return (
      <QueryClientProvider client={queryClient}>
        <UnauthenticatedProvider>{children}</UnauthenticatedProvider>
      </QueryClientProvider>
    );
  }

  return (
    <DynamicContextProvider
      settings={{
        environmentId: env.NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID as string,
        walletConnectors: [EthereumWalletConnectors],
        events: {
          onLogout: () => {
            // Clear every cached response so the next user never sees the
            // previous one's data flash on screen.
            queryClient.clear();
          },
          onAuthSuccess: () => {
            queryClient.invalidateQueries();
          },
        },
      }}
    >
      <QueryClientProvider client={queryClient}>
        <DynamicAuthBridge>{children}</DynamicAuthBridge>
      </QueryClientProvider>
    </DynamicContextProvider>
  );
}
