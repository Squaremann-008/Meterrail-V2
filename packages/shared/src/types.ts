/**
 * Wire types for the Meterrail API.
 *
 * These mirror the Go structs in `apps/api/internal/models` and the envelopes
 * in `apps/api/internal/httpx`. They are hand-maintained: when a Go struct's
 * JSON tags change, change the matching type here in the same commit.
 */

/** Successful responses are always wrapped in `{ data, meta? }`. */
export interface ApiEnvelope<T> {
  data: T;
  meta?: PaginationMeta;
}

export interface PaginationMeta {
  total: number;
  limit: number;
  offset: number;
  hasMore: boolean;
}

/** Failures are always `{ error: { code, message, details? } }`. */
export interface ApiErrorBody {
  error: {
    code: ApiErrorCode;
    message: string;
    details?: Record<string, unknown>;
  };
}

export type ApiErrorCode =
  | 'bad_request'
  | 'unauthorized'
  | 'forbidden'
  | 'not_found'
  | 'conflict'
  | 'unprocessable_entity'
  | 'rate_limited'
  | 'service_unavailable'
  | 'method_not_allowed'
  | 'timeout'
  | 'internal_error';

// --- identity -------------------------------------------------------------

export type Role = 'owner' | 'admin' | 'member' | 'viewer';
export type WalletChain = 'evm' | 'solana';

export interface Wallet {
  id: string;
  userId: string;
  address: string;
  chain: WalletChain;
  chainId?: number;
  provider?: string;
  isPrimary: boolean;
  verifiedAt?: string;
  createdAt: string;
  updatedAt: string;
}

export interface User {
  id: string;
  dynamicUserId: string;
  email?: string;
  username?: string;
  displayName: string;
  avatarUrl?: string;
  isActive: boolean;
  lastSeenAt?: string;
  wallets?: Wallet[];
  createdAt: string;
  updatedAt: string;
}

export interface Organization {
  id: string;
  slug: string;
  name: string;
  description?: string;
  logoUrl?: string;
  ownerId: string;
  createdAt: string;
  updatedAt: string;
}

export interface Membership {
  id: string;
  userId: string;
  organizationId: string;
  role: Role;
  user?: User;
  organization?: Organization;
  createdAt: string;
  updatedAt: string;
}

// --- media ----------------------------------------------------------------

export type MediaStatus = 'pending' | 'uploaded' | 'processing' | 'ready' | 'failed';

export interface MediaVariant {
  key: string;
  width: number;
  height: number;
  url: string;
  sizeBytes: number;
}

export interface MediaAsset {
  id: string;
  organizationId?: string;
  ownerId: string;
  key: string;
  bucket: string;
  filename: string;
  contentType: string;
  sizeBytes: number;
  checksum?: string;
  width?: number;
  height?: number;
  status: MediaStatus;
  publicUrl?: string;
  variants?: Record<string, MediaVariant>;
  metadata?: Record<string, unknown>;
  uploadedAt?: string;
  createdAt: string;
  updatedAt: string;
}

/** What the browser needs to PUT a file straight to Cloudflare R2. */
export interface PresignedUpload {
  key: string;
  url: string;
  method: string;
  headers: Record<string, string>;
  expiresAt: string;
  publicUrl?: string;
}

export interface UploadTicket {
  asset: MediaAsset;
  upload: PresignedUpload;
}

export interface RequestUploadInput {
  filename: string;
  contentType: string;
  sizeBytes: number;
  organizationId?: string;
}

// --- realtime sessions ----------------------------------------------------

export type SessionStatus = 'scheduled' | 'live' | 'ended' | 'cancelled';
export type ParticipantRole = 'publisher' | 'subscriber';

export interface SessionParticipant {
  id: string;
  sessionId: string;
  userId: string;
  agoraUid: number;
  role: ParticipantRole;
  joinedAt?: string;
  leftAt?: string;
  user?: User;
}

export interface Session {
  id: string;
  organizationId?: string;
  hostId: string;
  channelName: string;
  title: string;
  description?: string;
  status: SessionStatus;
  isRecorded: boolean;
  maxParticipants: number;
  scheduledFor?: string;
  startedAt?: string;
  endedAt?: string;
  host?: User;
  participants?: SessionParticipant[];
  createdAt: string;
  updatedAt: string;
}

export interface CreateSessionInput {
  title: string;
  description?: string;
  organizationId?: string;
  scheduledFor?: string;
  maxParticipants?: number;
  isRecorded?: boolean;
}

/** A scoped, short-lived Agora credential minted by the API. */
export interface AgoraToken {
  appId: string;
  channel: string;
  token: string;
  uid: number;
  role: ParticipantRole;
  expiresAt: string;
  expiresInMs: number;
}

export interface JoinSessionResult {
  session: Session;
  participant: SessionParticipant;
  rtc: AgoraToken;
  rtm?: AgoraToken;
}

// --- onchain --------------------------------------------------------------

export interface OnchainEvent {
  id: string;
  envioId: string;
  chainId: number;
  blockNumber: number;
  blockTimestamp: string;
  transactionHash: string;
  logIndex: number;
  contractAddress: string;
  eventName: string;
  payload: Record<string, unknown>;
  userId?: string;
  createdAt: string;
}

export interface IndexerChainStatus {
  chainId: number;
  indexerHead: number;
  mirroredBlock: number;
  lagBlocks: number;
  lastSyncedAt?: string;
}

// --- notifications --------------------------------------------------------

export type NotificationChannel = 'in_app' | 'email' | 'webhook';

export interface Notification {
  id: string;
  userId: string;
  channel: NotificationChannel;
  kind: string;
  title: string;
  body?: string;
  data?: Record<string, unknown>;
  readAt?: string;
  deliveredAt?: string;
  createdAt: string;
}

// --- health ---------------------------------------------------------------

export interface HealthCheck {
  status: 'ok' | 'error';
  latencyMs?: string;
  error?: string;
}

export interface HealthReport {
  status: 'ok' | 'degraded';
  version: string;
  commit: string;
  uptime: string;
  goroutines?: number;
  checks?: Record<string, HealthCheck>;
}

export interface QueueStats {
  queue: string;
  size: number;
  active: number;
  pending: number;
  scheduled: number;
  retry: number;
  archived: number;
  completed: number;
  processed: number;
  failed: number;
}
