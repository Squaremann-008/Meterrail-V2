'use client';

import Link from 'next/link';

import { useIndexerStatus, useIsAuthenticated, useMe, useSessions } from '@/hooks/use-api';
import { isVideoConfigured } from '@/lib/env';

export default function OverviewPage() {
  const isAuthenticated = useIsAuthenticated();
  const { data: user } = useMe();
  const { data: sessions } = useSessions({ limit: 5 });
  const { data: indexer } = useIndexerStatus();

  return (
    <div className="space-y-8">
      <section>
        <h1 className="text-2xl font-semibold tracking-tight">
          {user ? `Welcome back, ${user.displayName}` : 'Overview'}
        </h1>
        <p className="mt-1 text-sm text-[var(--color-ink-muted)]">
          {isAuthenticated
            ? 'Your workspace across sessions, media and onchain activity.'
            : 'Connect a wallet to see your sessions, uploads and notifications.'}
        </p>
      </section>

      <section className="grid gap-4 sm:grid-cols-3">
        <StatCard
          label="Upcoming sessions"
          value={sessions?.meta?.total ?? sessions?.items.length ?? 0}
          href="/sessions"
        />
        <StatCard label="Indexed chains" value={indexer?.length ?? 0} href="/onchain" />
        <StatCard label="Wallets linked" value={user?.wallets?.length ?? 0} />
      </section>

      {!isVideoConfigured && (
        <p className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-muted)] px-3 py-2 text-xs text-[var(--color-ink-muted)]">
          Video sessions are disabled: set <code className="font-mono">AGORA_APP_ID</code> and{' '}
          <code className="font-mono">AGORA_APP_CERTIFICATE</code> on the API.
        </p>
      )}

      <section>
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-semibold uppercase tracking-wide text-[var(--color-ink-muted)]">
            Recent sessions
          </h2>
          <Link href="/sessions" className="text-xs text-[var(--color-accent)] hover:underline">
            View all
          </Link>
        </div>

        {sessions?.items.length ? (
          <ul className="space-y-2">
            {sessions.items.map((session) => (
              <li key={session.id}>
                <Link
                  href={`/sessions/${session.id}`}
                  className="card flex items-center justify-between px-4 py-3 transition-colors hover:bg-[var(--color-surface-muted)]"
                >
                  <div>
                    <p className="text-sm font-medium">{session.title}</p>
                    <p className="text-xs text-[var(--color-ink-muted)]">
                      {session.scheduledFor
                        ? new Date(session.scheduledFor).toLocaleString()
                        : 'Not scheduled'}
                    </p>
                  </div>
                  <StatusPill status={session.status} />
                </Link>
              </li>
            ))}
          </ul>
        ) : (
          <p className="card px-4 py-8 text-center text-sm text-[var(--color-ink-muted)]">
            No sessions yet.
          </p>
        )}
      </section>
    </div>
  );
}

function StatCard({ label, value, href }: { label: string; value: number; href?: string }) {
  const content = (
    <div className="card px-4 py-5">
      <p className="text-xs uppercase tracking-wide text-[var(--color-ink-muted)]">{label}</p>
      <p className="mt-1 text-2xl font-semibold tabular-nums">{value}</p>
    </div>
  );

  return href ? (
    <Link href={href} className="block transition-opacity hover:opacity-80">
      {content}
    </Link>
  ) : (
    content
  );
}

export function StatusPill({ status }: { status: string }) {
  const tone: Record<string, string> = {
    live: 'bg-[var(--color-positive)]/15 text-[var(--color-positive)]',
    scheduled: 'bg-[var(--color-accent)]/15 text-[var(--color-accent)]',
    ended: 'bg-[var(--color-surface-muted)] text-[var(--color-ink-muted)]',
    cancelled: 'bg-[var(--color-danger)]/15 text-[var(--color-danger)]',
  };

  return (
    <span
      className={`rounded-full px-2 py-0.5 text-xs font-medium ${
        tone[status] ?? 'bg-[var(--color-surface-muted)] text-[var(--color-ink-muted)]'
      }`}
    >
      {status}
    </span>
  );
}
