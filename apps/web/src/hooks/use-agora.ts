'use client';

import type { IAgoraRTCClient, ICameraVideoTrack, IMicrophoneAudioTrack } from 'agora-rtc-sdk-ng';
import { useCallback, useEffect, useRef, useState } from 'react';
import { routes, type AgoraToken, type JoinSessionResult } from '@meterrail/shared';

import { api } from '@/lib/api-client';

/**
 * Renew this long before the token actually expires, so a slow network round
 * trip cannot leave the client holding an expired credential mid-call.
 */
const RENEW_LEAD_MS = 60_000;

/** Never schedule a renewal closer than this, to avoid a tight retry loop. */
const MIN_RENEW_DELAY_MS = 10_000;

export type CallStatus = 'idle' | 'connecting' | 'connected' | 'error';

/**
 * A remote participant, as plain data.
 *
 * Deliberately does **not** hold the SDK's `IAgoraRTCRemoteUser`. That object is
 * mutated in place by the SDK, so React cannot tell when it changes, and
 * reading it during render is a lint error for good reason. Components render
 * from these fields and attach the actual video track through `playRemoteVideo`
 * inside an effect.
 */
export interface RemoteParticipant {
  uid: string;
  hasVideo: boolean;
  hasAudio: boolean;
}

export interface UseAgoraCall {
  status: CallStatus;
  error: string | null;
  remoteUsers: RemoteParticipant[];
  isPublisher: boolean;
  isMicMuted: boolean;
  isCameraOff: boolean;
  attachLocalVideo: (element: HTMLDivElement | null) => void;
  playRemoteVideo: (uid: string, element: HTMLDivElement) => void;
  stopRemoteVideo: (uid: string) => void;
  join: (sessionId: string) => Promise<void>;
  leave: () => Promise<void>;
  toggleMic: () => Promise<void>;
  toggleCamera: () => Promise<void>;
}

/**
 * Drives one Agora RTC call.
 *
 * The SDK is imported lazily because it touches `window` at module scope and
 * would break server rendering. Tokens are minted by the Go API — the app
 * certificate never reaches the browser — and renewed automatically before they
 * lapse.
 */
export function useAgoraCall(): UseAgoraCall {
  const [status, setStatus] = useState<CallStatus>('idle');
  const [error, setError] = useState<string | null>(null);
  const [remoteUsers, setRemoteUsers] = useState<RemoteParticipant[]>([]);
  const [isPublisher, setIsPublisher] = useState(false);
  const [isMicMuted, setIsMicMuted] = useState(false);
  const [isCameraOff, setIsCameraOff] = useState(false);

  // The active credential. Storing it in state rather than a ref is what lets
  // the renewal effect below re-arm itself: each renewal sets a new token,
  // which re-runs the effect, which schedules the next one. No recursion.
  const [rtcToken, setRtcToken] = useState<AgoraToken | null>(null);

  const clientRef = useRef<IAgoraRTCClient | null>(null);
  const localAudioRef = useRef<IMicrophoneAudioTrack | null>(null);
  const localVideoTrackRef = useRef<ICameraVideoTrack | null>(null);
  const localContainerRef = useRef<HTMLDivElement | null>(null);
  const sessionIdRef = useRef<string | null>(null);

  /**
   * Callback ref: the container element can mount after the track exists (or
   * before), so play the local preview whenever both are available.
   */
  const attachLocalVideo = useCallback((element: HTMLDivElement | null) => {
    localContainerRef.current = element;
    if (element && localVideoTrackRef.current) {
      localVideoTrackRef.current.play(element);
    }
  }, []);

  /** Rebuilds the remote list from the SDK's state, as plain data. */
  const syncRemoteUsers = useCallback((client: IAgoraRTCClient) => {
    setRemoteUsers(
      client.remoteUsers.map((user) => ({
        uid: String(user.uid),
        hasVideo: user.hasVideo,
        hasAudio: user.hasAudio,
      })),
    );
  }, []);

  /** Attaches a remote participant's video to a DOM node. */
  const playRemoteVideo = useCallback((uid: string, element: HTMLDivElement) => {
    const client = clientRef.current;
    if (!client) return;

    const user = client.remoteUsers.find((candidate) => String(candidate.uid) === uid);
    user?.videoTrack?.play(element);
  }, []);

  const stopRemoteVideo = useCallback((uid: string) => {
    const client = clientRef.current;
    if (!client) return;

    const user = client.remoteUsers.find((candidate) => String(candidate.uid) === uid);
    user?.videoTrack?.stop();
  }, []);

  const leave = useCallback(async () => {
    localAudioRef.current?.stop();
    localAudioRef.current?.close();
    localAudioRef.current = null;

    localVideoTrackRef.current?.stop();
    localVideoTrackRef.current?.close();
    localVideoTrackRef.current = null;

    const client = clientRef.current;
    if (client) {
      client.removeAllListeners();
      // Leaving an already-left client throws; the call is over either way.
      try {
        await client.leave();
      } catch {
        /* ignore */
      }
      clientRef.current = null;
    }

    sessionIdRef.current = null;
    setRtcToken(null);
    setRemoteUsers([]);
    setStatus('idle');
    setIsPublisher(false);
    setIsMicMuted(false);
    setIsCameraOff(false);
  }, []);

  const join = useCallback(
    async (sessionId: string) => {
      setStatus('connecting');
      setError(null);

      try {
        // Ask the API to authorise us and mint a channel-scoped token.
        const joinResult = await api.post<JoinSessionResult>(routes.sessions.join(sessionId));
        const { rtc } = joinResult;

        // Loaded here rather than at module scope: the SDK needs `window`.
        const AgoraRTC = (await import('agora-rtc-sdk-ng')).default;
        AgoraRTC.setLogLevel(4); // errors only

        const client = AgoraRTC.createClient({ mode: 'rtc', codec: 'vp8' });
        clientRef.current = client;
        sessionIdRef.current = sessionId;

        client.on('user-published', async (user, mediaType) => {
          await client.subscribe(user, mediaType);
          if (mediaType === 'audio') {
            user.audioTrack?.play();
          }
          syncRemoteUsers(client);
        });

        client.on('user-unpublished', () => syncRemoteUsers(client));
        client.on('user-left', () => syncRemoteUsers(client));

        await client.join(rtc.appId, rtc.channel, rtc.token, rtc.uid);

        // Only publishers get a camera and microphone; subscribers watch.
        const publisher = rtc.role === 'publisher';
        if (publisher) {
          const [microphone, camera] = await AgoraRTC.createMicrophoneAndCameraTracks();
          localAudioRef.current = microphone;
          localVideoTrackRef.current = camera;

          if (localContainerRef.current) {
            camera.play(localContainerRef.current);
          }
          await client.publish([microphone, camera]);
        }

        setIsPublisher(publisher);
        setRtcToken(rtc);
        syncRemoteUsers(client);
        setStatus('connected');
      } catch (cause) {
        setStatus('error');
        setError(
          cause instanceof Error ? cause.message : 'Could not connect to the video session.',
        );
        // Do not leave a half-open client behind on failure.
        await leave();
      }
    },
    [leave, syncRemoteUsers],
  );

  // Token renewal. Runs once per issued token: schedule a refresh shortly
  // before expiry, and let the resulting state change arm the next one.
  useEffect(() => {
    const sessionId = sessionIdRef.current;
    if (!rtcToken || !sessionId) return;

    let cancelled = false;

    const renew = async () => {
      const client = clientRef.current;
      if (cancelled || !client) return;

      try {
        const renewed = await api.post<AgoraToken>(routes.sessions.renewToken(sessionId));
        if (cancelled) return;

        await client.renewToken(renewed.token);
        setRtcToken(renewed);
      } catch {
        if (!cancelled) {
          setError('Your session credential could not be renewed.');
        }
      }
    };

    const delay = Math.max(rtcToken.expiresInMs - RENEW_LEAD_MS, MIN_RENEW_DELAY_MS);
    const timer = setTimeout(renew, delay);

    // Agora also warns us directly, which covers a device that slept through
    // the timer above.
    const client = clientRef.current;
    client?.on('token-privilege-will-expire', renew);

    return () => {
      cancelled = true;
      clearTimeout(timer);
      client?.off('token-privilege-will-expire', renew);
    };
  }, [rtcToken]);

  const toggleMic = useCallback(async () => {
    const track = localAudioRef.current;
    if (!track) return;

    const nextMuted = !isMicMuted;
    await track.setEnabled(!nextMuted);
    setIsMicMuted(nextMuted);
  }, [isMicMuted]);

  const toggleCamera = useCallback(async () => {
    const track = localVideoTrackRef.current;
    if (!track) return;

    const nextOff = !isCameraOff;
    await track.setEnabled(!nextOff);
    setIsCameraOff(nextOff);
  }, [isCameraOff]);

  // Release the camera and microphone if the component unmounts mid-call.
  useEffect(() => {
    return () => {
      void leave();
    };
  }, [leave]);

  return {
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
  };
}
