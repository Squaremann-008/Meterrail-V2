'use client';

import { useState } from 'react';

import {
  useIsAuthenticated,
  useMarkAllNotificationsRead,
  useMarkNotificationRead,
  useNotifications,
  useUnreadCount,
} from '@/hooks/use-api';

export function NotificationBell() {
  const isAuthenticated = useIsAuthenticated();
  const [isOpen, setIsOpen] = useState(false);

  const { data: unread } = useUnreadCount();
  // Only fetch the list once the panel is actually opened.
  const { data: notifications, isLoading } = useNotifications({ limit: 10 });
  const markRead = useMarkNotificationRead();
  const markAllRead = useMarkAllNotificationsRead();

  if (!isAuthenticated) return null;

  const count = unread?.count ?? 0;

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setIsOpen((open) => !open)}
        aria-expanded={isOpen}
        aria-label={`Notifications${count > 0 ? `, ${count} unread` : ''}`}
        className="btn btn-secondary relative px-2.5 py-1.5"
      >
        <span aria-hidden>🔔</span>
        {count > 0 && (
          <span className="absolute -right-1 -top-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-[var(--color-danger)] px-1 text-[10px] font-semibold text-white">
            {count > 99 ? '99+' : count}
          </span>
        )}
      </button>

      {isOpen && (
        <div className="card absolute right-0 top-full z-20 mt-2 w-80 shadow-lg">
          <div className="flex items-center justify-between border-b border-[var(--color-border)] px-3 py-2">
            <span className="text-sm font-medium">Notifications</span>
            {count > 0 && (
              <button
                type="button"
                onClick={() => markAllRead.mutate()}
                disabled={markAllRead.isPending}
                className="text-xs text-[var(--color-accent)] hover:underline disabled:opacity-50"
              >
                Mark all read
              </button>
            )}
          </div>

          <div className="max-h-80 overflow-y-auto">
            {isLoading && (
              <p className="px-3 py-6 text-center text-xs text-[var(--color-ink-muted)]">
                Loading…
              </p>
            )}

            {!isLoading && (notifications?.items.length ?? 0) === 0 && (
              <p className="px-3 py-6 text-center text-xs text-[var(--color-ink-muted)]">
                Nothing here yet.
              </p>
            )}

            {notifications?.items.map((notification) => (
              <button
                key={notification.id}
                type="button"
                onClick={() => {
                  if (!notification.readAt) markRead.mutate(notification.id);
                }}
                className={`block w-full border-b border-[var(--color-border)] px-3 py-2.5 text-left last:border-0 hover:bg-[var(--color-surface-muted)] ${
                  notification.readAt ? 'opacity-60' : ''
                }`}
              >
                <p className="text-sm font-medium">{notification.title}</p>
                {notification.body && (
                  <p className="mt-0.5 text-xs text-[var(--color-ink-muted)]">
                    {notification.body}
                  </p>
                )}
                <time
                  dateTime={notification.createdAt}
                  className="mt-1 block text-[11px] text-[var(--color-ink-muted)]"
                >
                  {new Date(notification.createdAt).toLocaleString()}
                </time>
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
