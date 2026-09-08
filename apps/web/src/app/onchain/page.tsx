'use client';

import { useState } from 'react';

import { useIndexerStatus, useOnchainEvents } from '@/hooks/use-api';

export default function OnchainPage() {
  const [address, setAddress] = useState('');
  const { data: status } = useIndexerStatus();
  const { data: events, isLoading } = useOnchainEvents({
    address: address.trim() || undefined,
    limit: 50,
  });

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">Onchain activity</h1>
        <p className="mt-1 text-sm text-[var(--color-ink-muted)]">
          Events indexed by Envio HyperIndex and mirrored into Postgres, so reads stay available
          while the indexer resyncs.
        </p>
      </header>

      {status && status.length > 0 && (
        <section className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {status.map((chain) => (
            <div key={chain.chainId} className="card px-4 py-3">
              <p className="text-xs uppercase tracking-wide text-[var(--color-ink-muted)]">
                Chain {chain.chainId}
              </p>
              <p className="mt-1 text-lg font-semibold tabular-nums">
                {chain.mirroredBlock.toLocaleString()}
              </p>
              <p className="text-xs text-[var(--color-ink-muted)]">
                head {chain.indexerHead.toLocaleString()} ·{' '}
                <span
                  className={
                    chain.lagBlocks > 100
                      ? 'text-[var(--color-warning)]'
                      : 'text-[var(--color-positive)]'
                  }
                >
                  {chain.lagBlocks.toLocaleString()} behind
                </span>
              </p>
            </div>
          ))}
        </section>
      )}

      <div>
        <label htmlFor="address-filter" className="mb-1 block text-xs font-medium">
          Filter by address
        </label>
        <input
          id="address-filter"
          value={address}
          onChange={(event) => setAddress(event.target.value)}
          placeholder="0x…"
          spellCheck={false}
          className="w-full max-w-md rounded-md border border-[var(--color-border)] bg-[var(--color-canvas)] px-3 py-2 font-mono text-sm"
        />
      </div>

      {isLoading && <p className="text-sm text-[var(--color-ink-muted)]">Loading events…</p>}

      {!isLoading && (events?.items.length ?? 0) === 0 && (
        <p className="card px-4 py-10 text-center text-sm text-[var(--color-ink-muted)]">
          No indexed events yet. Start the indexer with{' '}
          <code className="font-mono">make indexer</code>.
        </p>
      )}

      {(events?.items.length ?? 0) > 0 && (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-[var(--color-border)] text-left text-xs uppercase tracking-wide text-[var(--color-ink-muted)]">
                <th className="px-4 py-2 font-medium">Block</th>
                <th className="px-4 py-2 font-medium">Event</th>
                <th className="px-4 py-2 font-medium">From</th>
                <th className="px-4 py-2 font-medium">To</th>
                <th className="px-4 py-2 font-medium">Tx</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--color-border)]">
              {events?.items.map((event) => (
                <tr key={event.id}>
                  <td className="px-4 py-2 tabular-nums">{event.blockNumber.toLocaleString()}</td>
                  <td className="px-4 py-2">{event.eventName}</td>
                  <td className="px-4 py-2 font-mono text-xs">
                    {truncate(String(event.payload.from ?? ''))}
                  </td>
                  <td className="px-4 py-2 font-mono text-xs">
                    {truncate(String(event.payload.to ?? ''))}
                  </td>
                  <td className="px-4 py-2 font-mono text-xs">{truncate(event.transactionHash)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

/** Shortens a hash or address to its recognisable head and tail. */
function truncate(value: string): string {
  if (value.length <= 14) return value || '—';
  return `${value.slice(0, 8)}…${value.slice(-6)}`;
}
