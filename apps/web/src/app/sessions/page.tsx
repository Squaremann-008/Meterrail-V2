'use client';

import Link from 'next/link';
import { useState, type FormEvent } from 'react';

import { StatusPill } from '@/app/page';
import { useCreateSession, useIsAuthenticated, useSessions } from '@/hooks/use-api';
import { ApiError } from '@/lib/api-client';

export default function SessionsPage() {
  const isAuthenticated = useIsAuthenticated();
  const [showMineOnly, setShowMineOnly] = useState(false);

  const { data, isLoading } = useSessions({
    mine: showMineOnly && isAuthenticated ? true : undefined,
    limit: 50,
  });

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Sessions</h1>
          <p className="mt-1 text-sm text-[var(--color-ink-muted)]">
            Realtime video rooms, authorised per participant and tokenised server-side.
          </p>
        </div>

        {isAuthenticated && (
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={showMineOnly}
              onChange={(event) => setShowMineOnly(event.target.checked)}
              className="size-4"
            />
            Only mine
          </label>
        )}
      </header>

      {isAuthenticated && <CreateSessionForm />}

      {isLoading && <p className="text-sm text-[var(--color-ink-muted)]">Loading sessions…</p>}

      {!isLoading && (data?.items.length ?? 0) === 0 && (
        <p className="card px-4 py-10 text-center text-sm text-[var(--color-ink-muted)]">
          No sessions yet.
        </p>
      )}

      <ul className="space-y-2">
        {data?.items.map((session) => (
          <li key={session.id}>
            <Link
              href={`/sessions/${session.id}`}
              className="card flex items-center justify-between px-4 py-3 transition-colors hover:bg-[var(--color-surface-muted)]"
            >
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">{session.title}</p>
                <p className="mt-0.5 truncate text-xs text-[var(--color-ink-muted)]">
                  {session.description || 'No description'} · up to {session.maxParticipants}{' '}
                  participants
                </p>
              </div>
              <StatusPill status={session.status} />
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}

function CreateSessionForm() {
  const createSession = useCreateSession();
  const [title, setTitle] = useState('');
  const [scheduledFor, setScheduledFor] = useState('');

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!title.trim()) return;

    createSession.mutate(
      {
        title: title.trim(),
        // datetime-local yields a local wall-clock string; the API expects an
        // absolute instant, so convert before sending.
        scheduledFor: scheduledFor ? new Date(scheduledFor).toISOString() : undefined,
      },
      {
        onSuccess: () => {
          setTitle('');
          setScheduledFor('');
        },
      },
    );
  }

  return (
    <form onSubmit={handleSubmit} className="card flex flex-wrap items-end gap-3 p-4">
      <div className="min-w-48 flex-1">
        <label htmlFor="session-title" className="mb-1 block text-xs font-medium">
          Title
        </label>
        <input
          id="session-title"
          value={title}
          onChange={(event) => setTitle(event.target.value)}
          placeholder="Weekly sync"
          maxLength={255}
          required
          className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-canvas)] px-3 py-2 text-sm"
        />
      </div>

      <div>
        <label htmlFor="session-schedule" className="mb-1 block text-xs font-medium">
          Starts at (optional)
        </label>
        <input
          id="session-schedule"
          type="datetime-local"
          value={scheduledFor}
          onChange={(event) => setScheduledFor(event.target.value)}
          className="rounded-md border border-[var(--color-border)] bg-[var(--color-canvas)] px-3 py-2 text-sm"
        />
      </div>

      <button type="submit" disabled={createSession.isPending} className="btn btn-primary">
        {createSession.isPending ? 'Creating…' : 'Create session'}
      </button>

      {createSession.error && (
        <p className="w-full text-xs text-[var(--color-danger)]">
          {createSession.error instanceof ApiError
            ? createSession.error.message
            : 'Could not create the session.'}
        </p>
      )}
    </form>
  );
}
