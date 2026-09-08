import type { ApiEnvelope, ApiErrorBody, ApiErrorCode, PaginationMeta } from '@meterrail/shared';

import { env } from './env';

/**
 * A typed error carrying the API's own error code, so callers can branch on
 * `err.code === 'forbidden'` instead of matching on message strings.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly code: ApiErrorCode;
  readonly details?: Record<string, unknown>;
  readonly requestId?: string;

  constructor(
    status: number,
    code: ApiErrorCode,
    message: string,
    details?: Record<string, unknown>,
    requestId?: string,
  ) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.details = details;
    this.requestId = requestId;
  }

  /** Auth failures should trigger a re-login rather than a retry. */
  get isAuthError(): boolean {
    return this.status === 401;
  }

  /** 5xx and 429 are worth retrying; 4xx generally is not. */
  get isRetryable(): boolean {
    return this.status >= 500 || this.status === 429;
  }
}

/** Supplies the current Dynamic JWT. Set once, at provider mount. */
type TokenGetter = () => string | null | undefined;

let getAuthToken: TokenGetter = () => null;

/**
 * Registers the token source. The Dynamic SDK owns the token and refreshes it,
 * so the client reads it per request rather than caching a copy that could go
 * stale mid-session.
 */
export function setAuthTokenGetter(getter: TokenGetter): void {
  getAuthToken = getter;
}

export interface RequestOptions extends Omit<RequestInit, 'body'> {
  /** Serialised as JSON unless it is already a BodyInit. */
  body?: unknown;
  /** Skip the Authorization header, for genuinely public endpoints. */
  anonymous?: boolean;
  /** Abort the request after this many milliseconds. */
  timeoutMs?: number;
}

const DEFAULT_TIMEOUT_MS = 20_000;

/**
 * The single fetch wrapper every API call goes through: attaches auth, encodes
 * JSON, enforces a timeout, and normalises both success and error shapes.
 */
async function request<T>(
  path: string,
  options: RequestOptions = {},
): Promise<{
  data: T;
  meta?: PaginationMeta;
}> {
  const { body, anonymous, timeoutMs = DEFAULT_TIMEOUT_MS, headers, signal, ...rest } = options;

  const url = path.startsWith('http') ? path : `${env.NEXT_PUBLIC_API_URL}${path}`;

  const requestHeaders = new Headers(headers);
  requestHeaders.set('Accept', 'application/json');

  if (!anonymous) {
    const token = getAuthToken();
    if (token) requestHeaders.set('Authorization', `Bearer ${token}`);
  }

  let payload: BodyInit | undefined;
  if (body !== undefined) {
    if (body instanceof FormData || body instanceof Blob || typeof body === 'string') {
      payload = body;
    } else {
      requestHeaders.set('Content-Type', 'application/json');
      payload = JSON.stringify(body);
    }
  }

  // Combine the caller's abort signal with our timeout so either can cancel.
  const timeoutSignal = AbortSignal.timeout(timeoutMs);
  const combinedSignal = signal ? AbortSignal.any([signal, timeoutSignal]) : timeoutSignal;

  let response: Response;
  try {
    response = await fetch(url, {
      ...rest,
      headers: requestHeaders,
      body: payload,
      signal: combinedSignal,
    });
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === 'TimeoutError') {
      throw new ApiError(408, 'timeout', `The request to ${path} timed out.`);
    }
    if (cause instanceof DOMException && cause.name === 'AbortError') {
      throw cause; // a deliberate cancellation, not a failure
    }
    throw new ApiError(
      0,
      'service_unavailable',
      'Could not reach the API. Check your connection and that the backend is running.',
    );
  }

  const requestId = response.headers.get('X-Request-Id') ?? undefined;

  if (response.status === 204) {
    return { data: undefined as T };
  }

  const raw = await response.text();
  let parsed: unknown;
  try {
    parsed = raw ? JSON.parse(raw) : null;
  } catch {
    throw new ApiError(
      response.status,
      'internal_error',
      `The API returned a non-JSON response (${response.status}).`,
      undefined,
      requestId,
    );
  }

  if (!response.ok) {
    const errorBody = parsed as ApiErrorBody | null;
    throw new ApiError(
      response.status,
      errorBody?.error?.code ?? 'internal_error',
      errorBody?.error?.message ?? `Request failed with status ${response.status}.`,
      errorBody?.error?.details,
      requestId,
    );
  }

  const envelope = parsed as ApiEnvelope<T>;
  return { data: envelope.data, meta: envelope.meta };
}

/** Returns just the payload, for the common case. */
async function unwrap<T>(path: string, options?: RequestOptions): Promise<T> {
  const { data } = await request<T>(path, options);
  return data;
}

/** Returns the payload alongside its pagination meta, for list endpoints. */
export interface PaginatedResult<T> {
  items: T;
  meta?: PaginationMeta;
}

async function paginated<T>(path: string, options?: RequestOptions): Promise<PaginatedResult<T>> {
  const { data, meta } = await request<T>(path, options);
  return { items: data, meta };
}

export const api = {
  get: <T>(path: string, options?: RequestOptions) =>
    unwrap<T>(path, { ...options, method: 'GET' }),

  list: <T>(path: string, options?: RequestOptions) =>
    paginated<T>(path, { ...options, method: 'GET' }),

  post: <T>(path: string, body?: unknown, options?: RequestOptions) =>
    unwrap<T>(path, { ...options, method: 'POST', body }),

  patch: <T>(path: string, body?: unknown, options?: RequestOptions) =>
    unwrap<T>(path, { ...options, method: 'PATCH', body }),

  put: <T>(path: string, body?: unknown, options?: RequestOptions) =>
    unwrap<T>(path, { ...options, method: 'PUT', body }),

  delete: <T = void>(path: string, options?: RequestOptions) =>
    unwrap<T>(path, { ...options, method: 'DELETE' }),
};
