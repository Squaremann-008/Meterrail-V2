'use client';

import { useQueryClient } from '@tanstack/react-query';
import { useRef, useState, type ChangeEvent } from 'react';

import { useDeleteMediaAsset, useIsAuthenticated, useMediaAssets } from '@/hooks/use-api';
import { ApiError } from '@/lib/api-client';
import { formatBytes, uploadFile, type UploadProgress } from '@/lib/upload';

export default function MediaPage() {
  const isAuthenticated = useIsAuthenticated();
  const { data, isLoading } = useMediaAssets({ limit: 50 });
  const deleteAsset = useDeleteMediaAsset();

  if (!isAuthenticated) {
    return (
      <p className="card px-4 py-10 text-center text-sm text-[var(--color-ink-muted)]">
        Connect a wallet to manage your uploads.
      </p>
    );
  }

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">Media</h1>
        <p className="mt-1 text-sm text-[var(--color-ink-muted)]">
          Files upload straight to Cloudflare R2 with a presigned URL — they never pass through the
          API server.
        </p>
      </header>

      <Uploader />

      {isLoading && <p className="text-sm text-[var(--color-ink-muted)]">Loading uploads…</p>}

      {!isLoading && (data?.items.length ?? 0) === 0 && (
        <p className="card px-4 py-10 text-center text-sm text-[var(--color-ink-muted)]">
          Nothing uploaded yet.
        </p>
      )}

      <ul className="card divide-y divide-[var(--color-border)]">
        {data?.items.map((asset) => (
          <li key={asset.id} className="flex items-center gap-4 px-4 py-3">
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium">{asset.filename}</p>
              <p className="text-xs text-[var(--color-ink-muted)]">
                {asset.contentType} · {formatBytes(asset.sizeBytes)} · {asset.status}
                {asset.variants && ` · ${Object.keys(asset.variants).length} variants`}
              </p>
            </div>

            {asset.publicUrl && (
              <a
                href={asset.publicUrl}
                target="_blank"
                rel="noreferrer"
                className="text-xs text-[var(--color-accent)] hover:underline"
              >
                Open
              </a>
            )}

            <button
              type="button"
              onClick={() => deleteAsset.mutate(asset.id)}
              disabled={deleteAsset.isPending}
              className="text-xs text-[var(--color-danger)] hover:underline disabled:opacity-50"
            >
              Delete
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

function Uploader() {
  const queryClient = useQueryClient();
  const inputRef = useRef<HTMLInputElement>(null);

  const [progress, setProgress] = useState<UploadProgress | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function handleChange(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    if (!file) return;

    setError(null);
    setProgress({ loaded: 0, total: file.size, percent: 0 });

    try {
      await uploadFile(file, { onProgress: setProgress });
      await queryClient.invalidateQueries({ queryKey: ['media'] });
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : 'The upload failed. Please try again.');
    } finally {
      setProgress(null);
      // Reset the input so re-selecting the same file fires a change event.
      if (inputRef.current) inputRef.current.value = '';
    }
  }

  const isUploading = progress !== null;

  return (
    <div className="card p-4">
      <label htmlFor="file-input" className="mb-2 block text-sm font-medium">
        Upload a file
      </label>
      <input
        id="file-input"
        ref={inputRef}
        type="file"
        onChange={handleChange}
        disabled={isUploading}
        accept="image/jpeg,image/png,image/webp,image/gif,image/avif,video/mp4,video/webm,application/pdf"
        className="block w-full text-sm text-[var(--color-ink-muted)] file:mr-3 file:rounded-md file:border-0 file:bg-[var(--color-accent)] file:px-3 file:py-1.5 file:text-sm file:text-[var(--color-accent-ink)]"
      />

      {isUploading && (
        <div className="mt-3">
          <div
            role="progressbar"
            aria-valuenow={progress.percent}
            aria-valuemin={0}
            aria-valuemax={100}
            className="h-1.5 overflow-hidden rounded-full bg-[var(--color-surface-muted)]"
          >
            <div
              className="h-full bg-[var(--color-accent)] transition-[width] duration-150"
              style={{ width: `${progress.percent}%` }}
            />
          </div>
          <p className="mt-1 text-xs text-[var(--color-ink-muted)]">
            {progress.percent}% · {formatBytes(progress.loaded)} of {formatBytes(progress.total)}
          </p>
        </div>
      )}

      {error && (
        <p role="alert" className="mt-2 text-xs text-[var(--color-danger)]">
          {error}
        </p>
      )}
    </div>
  );
}
