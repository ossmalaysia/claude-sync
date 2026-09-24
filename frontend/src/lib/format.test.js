import { describe, expect, it } from 'vitest';
import { countSelected, filterRows, formatBytes, pullProgressText, selectionMap } from './format.js';

describe('formatBytes', () => {
  it('formats sizes', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(1837431)).toBe('1.8 MB');
  });
});

describe('rows', () => {
  const rows = [
    { uuid: 'a', name: 'Acme: Website Revamp', selected: true },
    { uuid: 'b', name: 'Personal: Family', selected: false },
  ];
  it('filters case-insensitively and returns the same objects', () => {
    const out = filterRows(rows, '  website ');
    expect(out).toHaveLength(1);
    expect(out[0]).toBe(rows[0]);
    expect(filterRows(rows, '')).toBe(rows);
  });
  it('counts and maps the selection', () => {
    expect(countSelected(rows)).toBe(1);
    expect(selectionMap(rows)).toEqual({ a: true, b: false });
  });
});

describe('pullProgressText', () => {
  it('shows counts and MB', () => {
    expect(pullProgressText({ done: 3, total: 10, docs: 42, files: 7, bytes: 5 * 1024 * 1024, message: 'Acme: X' })).toBe(
      '3/10 projects · 42 docs, 7 files, 5.0 MB · Acme: X',
    );
  });
  it('handles missing totals', () => {
    expect(pullProgressText({ done: 0, total: 0, message: '' })).toBe('0/0 projects · 0 docs, 0 files, 0 B');
  });
});
