'use client';

import { useEffect, useRef, type ReactNode } from 'react';

import { useAgoraCall, type RemoteParticipant } from '@/hooks/use-agora';

export function VideoRoom({ sessionId, isHost }: { sessionId: string; isHost: boolean }) {
  // Destructured rather than kept as one `call` object on purpose: passing a
  // property of a hook's return value straight into `ref={}` makes React's
  // lint treat the whole object as ref-carrying, and every other read of it
  // then reports as a ref access during render.
  const {
    status,
    error,
    remoteUsers,
    isPublisher,
    isMicMuted,
    isCameraOff,
    attachLocalVideo,
    playRemoteVideo,
    stopRemoteVideo,
    join,
    leave,
    toggleMic,
    toggleCamera,
  } = useAgoraCall();

  const isConnected = status === 'connected';
  const participantCount = remoteUsers.length + (isPublisher ? 1 : 0);

  return (
    <section className="space-y-4">
      <div className="card overflow-hidden">
        <div className="grid gap-px bg-[var(--color-border)] sm:grid-cols-2">
          {/* Local preview: only publishers have a camera track to show. */}
          {isPublisher && (
            <Tile label="You">
              <div ref={attachLocalVideo} className="size-full bg-black" />
            </Tile>
          )}

          {remoteUsers.map((participant) => (
            <RemoteTile
              key={participant.uid}
              participant={participant}
              onPlay={playRemoteVideo}
              onStop={stopRemoteVideo}
            />
          ))}

          {isConnected && remoteUsers.length === 0 && (
            <Tile label="Waiting">
              <div className="flex size-full items-center justify-center bg-[var(--color-surface-muted)] text-xs text-[var(--color-ink-muted)]">
                Nobody else has joined yet.
              </div>
            </Tile>
          )}

          {!isConnected && !isPublisher && (
            <Tile label="Not connected">
              <div className="flex size-full items-center justify-center bg-[var(--color-surface-muted)] text-xs text-[var(--color-ink-muted)]">
                Join the call to see and hear other participants.
              </div>
            </Tile>
          )}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        {!isConnected ? (
          <button
            type="button"
            onClick={() => void join(sessionId)}
            disabled={status === 'connecting'}
            className="btn btn-primary"
          >
            {status === 'connecting' ? 'Connecting…' : 'Join call'}
          </button>
        ) : (
          <>
            <button type="button" onClick={() => void leave()} className="btn btn-danger">
              Leave call
            </button>

            {/* Only a publisher has tracks to toggle. */}
            {isPublisher && (
              <>
                <button
                  type="button"
                  onClick={() => void toggleMic()}
                  className="btn btn-secondary"
                  aria-pressed={isMicMuted}
                >
                  {isMicMuted ? 'Unmute mic' : 'Mute mic'}
                </button>
                <button
                  type="button"
                  onClick={() => void toggleCamera()}
                  className="btn btn-secondary"
                  aria-pressed={isCameraOff}
                >
                  {isCameraOff ? 'Turn camera on' : 'Turn camera off'}
                </button>
              </>
            )}
          </>
        )}

        <span className="ml-auto text-xs text-[var(--color-ink-muted)]">
          {isConnected
            ? `${participantCount} in call${isHost ? ' · you are the host' : ''}`
            : status}
        </span>
      </div>

      {error && (
        <p role="alert" className="text-xs text-[var(--color-danger)]">
          {error}
        </p>
      )}
    </section>
  );
}

function Tile({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="relative aspect-video bg-[var(--color-surface)]">
      {children}
      <span className="absolute bottom-2 left-2 rounded bg-black/60 px-1.5 py-0.5 text-[11px] text-white">
        {label}
      </span>
    </div>
  );
}

/**
 * Remote video has to be played into a DOM node the SDK owns. The hook holds
 * the SDK client, so attaching and detaching go through its callbacks — this
 * component only ever sees plain data.
 */
function RemoteTile({
  participant,
  onPlay,
  onStop,
}: {
  participant: RemoteParticipant;
  onPlay: (uid: string, element: HTMLDivElement) => void;
  onStop: (uid: string) => void;
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const { uid, hasVideo } = participant;

  useEffect(() => {
    const container = containerRef.current;
    if (!container || !hasVideo) return;

    onPlay(uid, container);
    return () => onStop(uid);
  }, [uid, hasVideo, onPlay, onStop]);

  return (
    <Tile label={`User ${uid}`}>
      <div ref={containerRef} className="size-full bg-black" />
      {!hasVideo && (
        <div className="absolute inset-0 flex items-center justify-center bg-[var(--color-surface-muted)] text-xs text-[var(--color-ink-muted)]">
          Camera off
        </div>
      )}
    </Tile>
  );
}
