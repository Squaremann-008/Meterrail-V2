import { describe, expect, it } from 'vitest';

import { queryKeys, routes } from './routes';

describe('query string construction', () => {
  it('omits the question mark when there are no params', () => {
    expect(routes.sessions.list()).toBe('/v1/sessions');
    expect(routes.media.list({})).toBe('/v1/media');
  });

  it('serialises the params that are set', () => {
    expect(routes.sessions.list({ limit: 10, offset: 20 })).toBe('/v1/sessions?limit=10&offset=20');
  });

  // Dropping empty values keeps `?q=` out of the URL when a search box is
  // cleared, which would otherwise be a distinct cache key from no search.
  it('drops undefined, null and empty-string values', () => {
    expect(routes.users.list({ q: '', limit: 25 })).toBe('/v1/users?limit=25');
    expect(routes.sessions.list({ status: undefined, limit: 5 })).toBe('/v1/sessions?limit=5');
  });

  it('keeps zero and false, which are meaningful values', () => {
    expect(routes.sessions.list({ offset: 0 })).toBe('/v1/sessions?offset=0');
    expect(routes.sessions.list({ mine: false })).toBe('/v1/sessions?mine=false');
  });

  it('encodes values that need escaping', () => {
    expect(routes.users.list({ q: 'ada lovelace' })).toBe('/v1/users?q=ada+lovelace');
    expect(routes.users.list({ q: 'a&b=c' })).toBe('/v1/users?q=a%26b%3Dc');
  });
});

describe('route paths', () => {
  it('builds the session lifecycle routes', () => {
    const id = '01a07fb3-c980-7d90-8dc6-da925db0b135';

    expect(routes.sessions.detail(id)).toBe(`/v1/sessions/${id}`);
    expect(routes.sessions.join(id)).toBe(`/v1/sessions/${id}/join`);
    expect(routes.sessions.renewToken(id)).toBe(`/v1/sessions/${id}/token`);
    expect(routes.sessions.start(id)).toBe(`/v1/sessions/${id}/start`);
    expect(routes.sessions.end(id)).toBe(`/v1/sessions/${id}/end`);
  });

  it('builds the media upload routes', () => {
    const id = 'asset-1';

    expect(routes.media.requestUpload).toBe('/v1/media/uploads');
    expect(routes.media.confirm(id)).toBe(`/v1/media/${id}/confirm`);
    expect(routes.media.downloadUrl(id, 300)).toBe(`/v1/media/${id}/download-url?ttlSeconds=300`);
    expect(routes.media.downloadUrl(id)).toBe(`/v1/media/${id}/download-url`);
  });

  it('keeps the health probes outside the versioned prefix', () => {
    expect(routes.health.live).toBe('/health/live');
    expect(routes.health.ready).toBe('/health/ready');
  });
});

describe('query keys', () => {
  // An invalidation of ['sessions'] must match every sessions list, whatever
  // filters produced it, so the resource name has to lead the key.
  it('lead with the resource name so prefix invalidation works', () => {
    expect(queryKeys.sessions({ mine: true })[0]).toBe('sessions');
    expect(queryKeys.onchainEvents({ chainId: 1 })[0]).toBe('onchain');
    expect(queryKeys.notifications({ unread: true })[0]).toBe('notifications');
  });

  it('distinguishes different params', () => {
    expect(queryKeys.sessions({ mine: true })).not.toEqual(queryKeys.sessions({ mine: false }));
  });

  it('treats absent params as an empty object, not undefined', () => {
    expect(queryKeys.sessions()).toEqual(['sessions', {}]);
  });
});
