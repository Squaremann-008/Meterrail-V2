'use client';

import { useDynamicContext, useIsLoggedIn } from '@dynamic-labs/sdk-react-core';
import { createContext, useContext, useMemo, type ReactNode } from 'react';

export interface AuthState {
  /** True once Dynamic has a verified session we can send to the API. */
  isAuthenticated: boolean;
  /** False while the SDK is still restoring a session from storage. */
  isReady: boolean;
}

const AuthContext = createContext<AuthState>({ isAuthenticated: false, isReady: true });

/**
 * The rest of the app reads auth state from here rather than calling the
 * Dynamic hooks directly.
 *
 * Those hooks throw ("Store not initialized") whenever no
 * DynamicContextProvider is mounted — which is the case during static
 * prerendering and whenever NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID is unset. This
 * indirection keeps both situations rendering an ordinary signed-out page.
 */
export function useAuth(): AuthState {
  return useContext(AuthContext);
}

export function useIsAuthenticated(): boolean {
  return useAuth().isAuthenticated;
}

/** Supplies the signed-out state when Dynamic is not configured. */
export function UnauthenticatedProvider({ children }: { children: ReactNode }) {
  const value = useMemo<AuthState>(() => ({ isAuthenticated: false, isReady: true }), []);
  return <AuthContext value={value}>{children}</AuthContext>;
}

/**
 * Bridges Dynamic's state onto the context. Only ever rendered inside a
 * DynamicContextProvider, so the SDK hooks below are always safe here.
 */
export function DynamicAuthBridge({ children }: { children: ReactNode }) {
  const isLoggedIn = useIsLoggedIn();
  const { sdkHasLoaded } = useDynamicContext();

  const value = useMemo<AuthState>(
    () => ({
      isAuthenticated: Boolean(sdkHasLoaded && isLoggedIn),
      isReady: Boolean(sdkHasLoaded),
    }),
    [sdkHasLoaded, isLoggedIn],
  );

  return <AuthContext value={value}>{children}</AuthContext>;
}
