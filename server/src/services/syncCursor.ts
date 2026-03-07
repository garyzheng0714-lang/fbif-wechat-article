import { readFileSync, writeFileSync, existsSync, unlinkSync } from 'fs';
import { resolve } from 'path';

export interface SyncCursor {
  oldestSyncedDate: string;
  newestSyncedDate: string;
  backfillComplete: boolean;
}

function getCursorPaths(): string[] {
  return [
    resolve(process.cwd(), '.sync-cursor.json'),
    resolve(process.cwd(), '..', '.sync-cursor.json'),
  ];
}

export function readCursor(): SyncCursor | null {
  for (const p of getCursorPaths()) {
    if (existsSync(p)) {
      const raw = readFileSync(p, 'utf-8');
      return JSON.parse(raw) as SyncCursor;
    }
  }
  return null;
}

export function writeCursor(cursor: SyncCursor): void {
  const p = getCursorPaths()[0];
  writeFileSync(p, JSON.stringify(cursor, null, 2), 'utf-8');
  console.log(`[Cursor] Saved: oldest=${cursor.oldestSyncedDate}, newest=${cursor.newestSyncedDate}, complete=${cursor.backfillComplete}`);
}

export function deleteCursor(): void {
  for (const p of getCursorPaths()) {
    if (existsSync(p)) {
      unlinkSync(p);
      console.log(`[Cursor] Deleted: ${p}`);
    }
  }
}
