import { describe, expect, it } from 'vitest';

import { formatBytes } from './upload';

describe('formatBytes', () => {
  it('renders zero without a fractional part', () => {
    expect(formatBytes(0)).toBe('0 B');
  });

  it('renders bytes as whole numbers', () => {
    expect(formatBytes(1)).toBe('1 B');
    expect(formatBytes(999)).toBe('999 B');
  });

  it('steps up a unit at each 1024 boundary', () => {
    expect(formatBytes(1024)).toBe('1.0 KB');
    expect(formatBytes(1024 * 1024)).toBe('1.0 MB');
    expect(formatBytes(1024 * 1024 * 1024)).toBe('1.0 GB');
  });

  it('keeps one decimal place above the byte unit', () => {
    expect(formatBytes(1536)).toBe('1.5 KB');
    expect(formatBytes(2_621_440)).toBe('2.5 MB');
  });

  // The R2 upload limit is 25 MiB; this is the number the UI shows when a file
  // is rejected, so it needs to read correctly.
  it('formats the upload size limit', () => {
    expect(formatBytes(26_214_400)).toBe('25.0 MB');
  });

  // Anything past the largest unit stays in that unit rather than falling off
  // the end of the array and rendering "undefined".
  it('clamps to the largest known unit', () => {
    expect(formatBytes(1024 ** 4)).toBe('1024.0 GB');
  });
});
