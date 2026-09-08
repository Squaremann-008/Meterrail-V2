'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  queryKeys,
  routes,
  type CreateSessionInput,
  type IndexerChainStatus,
  type JoinSessionResult,
  type MediaAsset,
  type Notification,
  type OnchainEvent,
  type Organization,
  type Session,
  type User,
} from '@meterrail/shared';

import { useIsAuthenticated } from '@/components/auth-context';
import { api } from '@/lib/api-client';

/**
 * Re-exported so callers have one import for "can I call the API yet". Every
 * authenticated query gates on it, which keeps React Query from firing a burst
 * of 401s while the Dynamic SDK is still restoring the session.
 */
export { useIsAuthenticated };

// --- identity -------------------------------------------------------------

/** The signed-in user. Also the call that provisions them on first login. */
export function useMe() {
  const enabled = useIsAuthenticated();

  return useQuery({
    queryKey: queryKeys.me,
    queryFn: () => api.get<User>(routes.me.profile),
    enabled,
  });
}

export function useMyOrganizations() {
  const enabled = useIsAuthenticated();

  return useQuery({
    queryKey: queryKeys.myOrganizations,
    queryFn: () => api.get<Organization[]>(routes.me.organizations),
    enabled,
  });
}

export function useUpdateProfile() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: { displayName?: string; username?: string; avatarUrl?: string }) =>
      api.patch<User>(routes.me.profile, input),
    onSuccess: (user) => {
      // Seed the cache from the response instead of refetching.
      queryClient.setQueryData(queryKeys.me, user);
    },
  });
}

// --- sessions -------------------------------------------------------------

export interface SessionListParams {
  status?: string;
  organizationId?: string;
  mine?: boolean;
  limit?: number;
  offset?: number;
}

export function useSessions(params: SessionListParams = {}) {
  return useQuery({
    queryKey: queryKeys.sessions({ ...params }),
    queryFn: () => api.list<Session[]>(routes.sessions.list(params)),
  });
}

export function useSession(sessionId: string | undefined) {
  return useQuery({
    queryKey: queryKeys.session(sessionId ?? ''),
    queryFn: () => api.get<Session>(routes.sessions.detail(sessionId as string)),
    enabled: Boolean(sessionId),
  });
}

export function useCreateSession() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: CreateSessionInput) => api.post<Session>(routes.sessions.create, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sessions'] });
    },
  });
}

/** Requests Agora credentials for a session. */
export function useJoinSession() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (sessionId: string) => api.post<JoinSessionResult>(routes.sessions.join(sessionId)),
    onSuccess: (result) => {
      queryClient.setQueryData(queryKeys.session(result.session.id), result.session);
    },
  });
}

export function useSessionLifecycle(sessionId: string) {
  const queryClient = useQueryClient();

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: queryKeys.session(sessionId) });
    queryClient.invalidateQueries({ queryKey: ['sessions'] });
  };

  const start = useMutation({
    mutationFn: () => api.post<Session>(routes.sessions.start(sessionId)),
    onSuccess: invalidate,
  });

  const end = useMutation({
    mutationFn: () => api.post<Session>(routes.sessions.end(sessionId)),
    onSuccess: invalidate,
  });

  const leave = useMutation({
    mutationFn: () => api.post<void>(routes.sessions.leave(sessionId)),
    onSuccess: invalidate,
  });

  return { start, end, leave };
}

// --- media ----------------------------------------------------------------

export function useMediaAssets(params: { limit?: number; offset?: number } = {}) {
  const enabled = useIsAuthenticated();

  return useQuery({
    queryKey: queryKeys.media(params),
    queryFn: () => api.list<MediaAsset[]>(routes.media.list(params)),
    enabled,
  });
}

export function useDeleteMediaAsset() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (assetId: string) => api.delete(routes.media.detail(assetId)),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['media'] });
    },
  });
}

// --- onchain --------------------------------------------------------------

export interface OnchainParams {
  chainId?: number;
  eventName?: string;
  contract?: string;
  address?: string;
  limit?: number;
  offset?: number;
}

export function useOnchainEvents(params: OnchainParams = {}) {
  return useQuery({
    queryKey: queryKeys.onchainEvents({ ...params }),
    queryFn: () => api.list<OnchainEvent[]>(routes.onchain.events(params)),
  });
}

export function useIndexerStatus() {
  return useQuery({
    queryKey: queryKeys.onchainStatus,
    queryFn: () => api.get<IndexerChainStatus[]>(routes.onchain.status),
    // The indexer moves with the chain, so poll rather than cache long.
    refetchInterval: 30_000,
    staleTime: 15_000,
  });
}

// --- notifications --------------------------------------------------------

export function useNotifications(params: { unread?: boolean; limit?: number } = {}) {
  const enabled = useIsAuthenticated();

  return useQuery({
    queryKey: queryKeys.notifications(params),
    queryFn: () => api.list<Notification[]>(routes.notifications.list(params)),
    enabled,
  });
}

export function useUnreadCount() {
  const enabled = useIsAuthenticated();

  return useQuery({
    queryKey: queryKeys.unreadCount,
    queryFn: () => api.get<{ count: number }>(routes.notifications.unreadCount),
    enabled,
    refetchInterval: 60_000,
  });
}

export function useMarkNotificationRead() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (notificationId: string) =>
      api.post<void>(routes.notifications.markRead(notificationId)),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['notifications'] });
    },
  });
}

export function useMarkAllNotificationsRead() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => api.post<{ updated: number }>(routes.notifications.markAllRead),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['notifications'] });
    },
  });
}
