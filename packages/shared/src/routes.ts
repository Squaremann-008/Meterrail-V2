/**
 * The API route table, in one place.
 *
 * Keeping paths here rather than inline at call sites means a route rename in
 * the Go router is a single-file change on the TypeScript side, and it keeps
 * query-string construction consistent.
 */

export const API_VERSION = 'v1' as const;

type QueryValue = string | number | boolean | undefined | null;

/**
 * Serialises pagination and filter params, dropping empty values.
 *
 * Generic over the params object rather than typed as an index signature, so
 * plain interfaces (which have no implicit index signature) can be passed
 * without a cast at every call site.
 */
function query<T extends object>(params: T): string {
  const search = new URLSearchParams();

  for (const [key, value] of Object.entries(params) as [string, QueryValue][]) {
    if (value === undefined || value === null || value === '') continue;
    search.set(key, String(value));
  }

  const serialised = search.toString();
  return serialised ? `?${serialised}` : '';
}

export interface ListParams {
  limit?: number;
  offset?: number;
}

export const routes = {
  health: {
    live: '/health/live',
    ready: '/health/ready',
  },

  me: {
    profile: `/${API_VERSION}/me`,
    organizations: `/${API_VERSION}/me/organizations`,
  },

  users: {
    list: (params: ListParams & { q?: string } = {}) => `/${API_VERSION}/users${query(params)}`,
    detail: (userId: string) => `/${API_VERSION}/users/${userId}`,
  },

  media: {
    list: (params: ListParams = {}) => `/${API_VERSION}/media${query(params)}`,
    requestUpload: `/${API_VERSION}/media/uploads`,
    confirm: (assetId: string) => `/${API_VERSION}/media/${assetId}/confirm`,
    detail: (assetId: string) => `/${API_VERSION}/media/${assetId}`,
    downloadUrl: (assetId: string, ttlSeconds?: number) =>
      `/${API_VERSION}/media/${assetId}/download-url${query({ ttlSeconds })}`,
  },

  sessions: {
    list: (
      params: ListParams & { status?: string; organizationId?: string; mine?: boolean } = {},
    ) => `/${API_VERSION}/sessions${query(params)}`,
    create: `/${API_VERSION}/sessions`,
    detail: (sessionId: string) => `/${API_VERSION}/sessions/${sessionId}`,
    join: (sessionId: string) => `/${API_VERSION}/sessions/${sessionId}/join`,
    renewToken: (sessionId: string) => `/${API_VERSION}/sessions/${sessionId}/token`,
    leave: (sessionId: string) => `/${API_VERSION}/sessions/${sessionId}/leave`,
    start: (sessionId: string) => `/${API_VERSION}/sessions/${sessionId}/start`,
    end: (sessionId: string) => `/${API_VERSION}/sessions/${sessionId}/end`,
  },

  onchain: {
    events: (
      params: ListParams & {
        chainId?: number;
        eventName?: string;
        contract?: string;
        address?: string;
      } = {},
    ) => `/${API_VERSION}/onchain/events${query(params)}`,
    status: `/${API_VERSION}/onchain/status`,
  },

  notifications: {
    list: (params: ListParams & { unread?: boolean } = {}) =>
      `/${API_VERSION}/notifications${query(params)}`,
    unreadCount: `/${API_VERSION}/notifications/unread-count`,
    markRead: (notificationId: string) => `/${API_VERSION}/notifications/${notificationId}/read`,
    markAllRead: `/${API_VERSION}/notifications/read-all`,
  },
} as const;

/**
 * React Query cache keys, derived from the same shapes as the routes so an
 * invalidation cannot drift from the query it is meant to clear.
 */
export const queryKeys = {
  me: ['me'] as const,
  myOrganizations: ['me', 'organizations'] as const,
  users: (params?: ListParams & { q?: string }) => ['users', params ?? {}] as const,
  user: (userId: string) => ['users', userId] as const,
  media: (params?: ListParams) => ['media', params ?? {}] as const,
  mediaAsset: (assetId: string) => ['media', assetId] as const,
  sessions: (params?: Record<string, unknown>) => ['sessions', params ?? {}] as const,
  session: (sessionId: string) => ['sessions', sessionId] as const,
  onchainEvents: (params?: Record<string, unknown>) => ['onchain', 'events', params ?? {}] as const,
  onchainStatus: ['onchain', 'status'] as const,
  notifications: (params?: Record<string, unknown>) => ['notifications', params ?? {}] as const,
  unreadCount: ['notifications', 'unread-count'] as const,
  health: ['health'] as const,
} as const;
