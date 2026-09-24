export function formatBytes(n) {
  if (!n) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

// Returns the same row objects so bind:checked mutates the originals.
export function filterRows(rows, query) {
  const q = query.trim().toLowerCase();
  return q ? rows.filter((r) => r.name.toLowerCase().includes(q)) : rows;
}

export function countSelected(rows) {
  return rows.filter((r) => r.selected).length;
}

export function selectionMap(rows) {
  return Object.fromEntries(rows.map((r) => [r.uuid, !!r.selected]));
}

// pullProgressText renders a pull progress event: projects done/total,
// running doc and file counts, and bytes downloaded so far.
export function pullProgressText(e) {
  const counts = `${e.docs || 0} docs, ${e.files || 0} files, ${formatBytes(e.bytes || 0)}`;
  return `${e.done}/${e.total} projects · ${counts}${e.message ? ` · ${e.message}` : ''}`;
}
