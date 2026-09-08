import { z } from 'zod';

/**
 * Treats an empty or whitespace-only variable as absent.
 *
 * `.env.example` ships the optional keys present but blank, and `make env`
 * copies it verbatim, so an unset value reaches this file as `''` rather than
 * `undefined`. Without this, `.optional()` would reject a freshly generated
 * `.env.local` and the build would fail on a correctly configured checkout.
 */
const blankAsAbsent = (value: unknown) =>
  typeof value === 'string' && value.trim() === '' ? undefined : value;

const optionalString = () => z.preprocess(blankAsAbsent, z.string().min(1).optional());

/**
 * Public runtime configuration.
 *
 * Next inlines `NEXT_PUBLIC_*` at build time, so every value must be referenced
 * by its full literal name — destructuring `process.env` would leave them
 * undefined in the browser bundle.
 */
const publicEnvSchema = z.object({
  NEXT_PUBLIC_API_URL: z.preprocess(blankAsAbsent, z.url().default('http://localhost:8080')),
  NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID: optionalString(),
  NEXT_PUBLIC_AGORA_APP_ID: optionalString(),
  NEXT_PUBLIC_CDN_HOSTNAME: optionalString(),
  NEXT_PUBLIC_APP_NAME: z.preprocess(blankAsAbsent, z.string().default('Meterrail')),
});

const parsed = publicEnvSchema.safeParse({
  NEXT_PUBLIC_API_URL: process.env.NEXT_PUBLIC_API_URL,
  NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID: process.env.NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID,
  NEXT_PUBLIC_AGORA_APP_ID: process.env.NEXT_PUBLIC_AGORA_APP_ID,
  NEXT_PUBLIC_CDN_HOSTNAME: process.env.NEXT_PUBLIC_CDN_HOSTNAME,
  NEXT_PUBLIC_APP_NAME: process.env.NEXT_PUBLIC_APP_NAME,
});

if (!parsed.success) {
  // Failing loudly at module load beats a runtime crash three screens deep.
  throw new Error(
    `Invalid public environment configuration:\n${z.prettifyError(parsed.error)}\n` +
      'Copy apps/web/.env.example to apps/web/.env.local and fill in the missing values.',
  );
}

export const env = parsed.data;

/**
 * Whether wallet auth can actually run. Without a Dynamic environment id the
 * app still renders — it just shows a configuration notice instead of a connect
 * button, which is far easier to debug than a blank page.
 */
export const isAuthConfigured = Boolean(env.NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID);

/** Whether the video features should be offered. */
export const isVideoConfigured = Boolean(env.NEXT_PUBLIC_AGORA_APP_ID);
