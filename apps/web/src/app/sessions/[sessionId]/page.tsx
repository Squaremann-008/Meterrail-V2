'use client';

import { useParams } from 'next/navigation';

import { StatusPill } from '@/app/page';
import { VideoRoom } from '@/components/video-room';
import { useIsAuthenticated, useMe, useSession, useSessionLifecycle } from '@/hooks/use-api';

export default function SessionDetailPage() {
  const params = useParams<{ sessionId: string }>();
  const sessionId = params.sessionId;

  const isAuthenticated = useIsAuthenticated();
  const { data: user } = useMe();
  const { data: session, isLoading, error } = useSession(sessionId);
  const { start, end } = useSessionLifecycle(sessionId);

  if (isLoading) {
    return <p className="text-sm text-[var(--color-ink-muted)]">Loading session…</p>;
  }

  if (error || !session) {
    return (
      <p className="card px-4 py-10 text-center text-sm text-[var(--color-danger)]">
        This session could not be loaded.
      </p>
    );
  }

  const isHost = Boolean(user && session.hostId === user.id);
  const isFinished = session.status === 'ended' || session.status === 'cancelled';

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-semibold tracking-tight">{session.title}</h1>
            <StatusPill status={session.status} />
          </div>
          {session.description && (
            <p className="mt-1 text-sm text-[var(--color-ink-muted)]">{session.description}</p>
          )}
          <p className="mt-1 font-mono text-xs text-[var(--color-ink-muted)]">
            channel {session.channelName}
          </p>
        </div>

        {isHost && !isFinished && (
          <div className="flex gap-2">
            {session.status === 'scheduled' && (
              <button
                type="button"
                onClick={() => start.mutate()}
                disabled={start.isPending}
                className="btn btn-primary"
              >
                {start.isPending ? 'Starting…' : 'Go live'}
              </button>
            )}
            {session.status === 'live' && (
              <button
                type="button"
                onClick={() => end.mutate()}
                disabled={end.isPending}
                className="btn btn-danger"
              >
                {end.isPending ? 'Ending…' : 'End session'}
              </button>
            )}
          </div>
        )}
      </header>

      {isFinished ? (
        <p className="card px-4 py-10 text-center text-sm text-[var(--color-ink-muted)]">
          This session has finished.
        </p>
      ) : isAuthenticated ? (
        <VideoRoom sessionId={sessionId} isHost={isHost} />
      ) : (
        <p className="card px-4 py-10 text-center text-sm text-[var(--color-ink-muted)]">
          Connect a wallet to join this session.
        </p>
      )}

      {session.participants && session.participants.length > 0 && (
        <section>
          <h2 className="mb-2 text-sm font-semibold uppercase tracking-wide text-[var(--color-ink-muted)]">
            Participants
          </h2>
          <ul className="card divide-y divide-[var(--color-border)]">
            {session.participants.map((participant) => (
              <li key={participant.id} className="flex items-center justify-between px-4 py-2.5">
                <span className="text-sm">
                  {participant.user?.displayName ?? participant.userId}
                </span>
                <span className="text-xs text-[var(--color-ink-muted)]">
                  {participant.role}
                  {participant.leftAt ? ' · left' : participant.joinedAt ? ' · in call' : ''}
                </span>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
