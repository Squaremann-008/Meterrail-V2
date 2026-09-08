import { routes, type MediaAsset, type UploadTicket } from '@meterrail/shared';

import { api, ApiError } from './api-client';

/**
 * Direct-to-R2 upload.
 *
 * The file never touches the Go API: it asks for a presigned PUT, uploads to
 * Cloudflare from the browser, then tells the API to verify and record it.
 * That keeps large uploads off the application server entirely.
 */
export interface UploadProgress {
  loaded: number;
  total: number;
  /** 0–100, rounded. */
  percent: number;
}

export interface UploadOptions {
  organizationId?: string;
  onProgress?: (progress: UploadProgress) => void;
  signal?: AbortSignal;
}

export async function uploadFile(file: File, options: UploadOptions = {}): Promise<MediaAsset> {
  // 1. Reserve a key and get a presigned PUT.
  const ticket = await api.post<UploadTicket>(routes.media.requestUpload, {
    filename: file.name,
    contentType: file.type || 'application/octet-stream',
    sizeBytes: file.size,
    organizationId: options.organizationId,
  });

  // 2. PUT the bytes straight to Cloudflare.
  await putToStorage(file, ticket, options);

  // 3. Have the API verify the object landed and queue post-processing.
  return api.post<MediaAsset>(routes.media.confirm(ticket.asset.id));
}

/**
 * XMLHttpRequest rather than fetch: it is still the only browser API that
 * reports upload progress, which matters for multi-megabyte files.
 */
function putToStorage(file: File, ticket: UploadTicket, options: UploadOptions): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open(ticket.upload.method, ticket.upload.url, true);

    // Every signed header must be replayed exactly or R2 rejects the signature.
    for (const [name, value] of Object.entries(ticket.upload.headers)) {
      xhr.setRequestHeader(name, value);
    }

    if (options.onProgress) {
      xhr.upload.addEventListener('progress', (event) => {
        if (!event.lengthComputable) return;
        options.onProgress?.({
          loaded: event.loaded,
          total: event.total,
          percent: Math.round((event.loaded / event.total) * 100),
        });
      });
    }

    xhr.addEventListener('load', () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve();
        return;
      }
      reject(
        new ApiError(
          xhr.status,
          'service_unavailable',
          `Object storage rejected the upload (${xhr.status}). The presigned URL may have expired.`,
        ),
      );
    });

    xhr.addEventListener('error', () => {
      reject(new ApiError(0, 'service_unavailable', 'The upload failed to reach object storage.'));
    });

    xhr.addEventListener('abort', () => {
      reject(new DOMException('Upload aborted', 'AbortError'));
    });

    if (options.signal) {
      if (options.signal.aborted) {
        xhr.abort();
        return;
      }
      options.signal.addEventListener('abort', () => xhr.abort(), { once: true });
    }

    xhr.send(file);
  });
}

/** Human-readable byte size for upload UIs. */
export function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';

  const units = ['B', 'KB', 'MB', 'GB'];
  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** exponent;

  return `${value.toFixed(exponent === 0 ? 0 : 1)} ${units[exponent]}`;
}
