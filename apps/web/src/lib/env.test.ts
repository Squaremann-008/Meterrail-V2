import { describe, expect, it } from 'vitest';
import { z } from 'zod';

/**
 * Mirrors the schema in `env.ts`.
 *
 * `env.ts` reads `process.env` at module load and throws on bad input, so it
 * cannot be re-imported per case. The rules under test are the preprocessing
 * ones, which are what actually broke: `.env.example` ships optional keys
 * present but blank, `make env` copies it verbatim, and a blank value reaches
 * the schema as `''` rather than `undefined`.
 */
const blankAsAbsent = (value: unknown) =>
  typeof value === 'string' && value.trim() === '' ? undefined : value;

const optionalString = () => z.preprocess(blankAsAbsent, z.string().min(1).optional());

const schema = z.object({
  NEXT_PUBLIC_API_URL: z.preprocess(blankAsAbsent, z.url().default('http://localhost:8080')),
  NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID: optionalString(),
  NEXT_PUBLIC_AGORA_APP_ID: optionalString(),
  NEXT_PUBLIC_APP_NAME: z.preprocess(blankAsAbsent, z.string().default('Meterrail')),
});

describe('public environment schema', () => {
  it('accepts a freshly copied .env.example, where optional keys are blank', () => {
    const result = schema.safeParse({
      NEXT_PUBLIC_API_URL: 'http://localhost:8080',
      NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID: '',
      NEXT_PUBLIC_AGORA_APP_ID: '',
      NEXT_PUBLIC_APP_NAME: 'Meterrail',
    });

    expect(result.success).toBe(true);
    expect(result.data?.NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID).toBeUndefined();
    expect(result.data?.NEXT_PUBLIC_AGORA_APP_ID).toBeUndefined();
  });

  it('accepts a completely empty environment and falls back to defaults', () => {
    const result = schema.safeParse({});

    expect(result.success).toBe(true);
    expect(result.data?.NEXT_PUBLIC_API_URL).toBe('http://localhost:8080');
    expect(result.data?.NEXT_PUBLIC_APP_NAME).toBe('Meterrail');
  });

  it('treats a whitespace-only value as absent', () => {
    const result = schema.safeParse({ NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID: '   ' });

    expect(result.success).toBe(true);
    expect(result.data?.NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID).toBeUndefined();
  });

  it('keeps a real value', () => {
    const result = schema.safeParse({ NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID: 'env-abc-123' });

    expect(result.data?.NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID).toBe('env-abc-123');
  });

  // A typo'd API URL should fail at build time, not produce a broken bundle.
  it('still rejects a malformed API URL', () => {
    expect(schema.safeParse({ NEXT_PUBLIC_API_URL: 'not-a-url' }).success).toBe(false);
  });

  it('drives the feature flags from presence, not from empty strings', () => {
    const blank = schema.parse({ NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID: '' });
    const set = schema.parse({ NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID: 'env-abc-123' });

    expect(Boolean(blank.NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID)).toBe(false);
    expect(Boolean(set.NEXT_PUBLIC_DYNAMIC_ENVIRONMENT_ID)).toBe(true);
  });
});
