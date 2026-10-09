import { ChangeType, ReviewStatus } from './models';

/** Derives a file status from its hunk statuses (same rule as the Go backend). */
export function aggregateStatus(statuses: readonly ReviewStatus[]): ReviewStatus {
  let reviewed = 0;
  for (const s of statuses) {
    if (s === 'needs_attention') {
      return 'needs_attention';
    }
    if (s === 'reviewed') {
      reviewed++;
    }
  }
  return reviewed === statuses.length ? 'reviewed' : 'unreviewed';
}

export const STATUS_ICON: Record<ReviewStatus, string> = {
  unreviewed: 'pi pi-circle',
  reviewed: 'pi pi-check-circle',
  needs_attention: 'pi pi-flag-fill',
};

export const STATUS_LABEL: Record<ReviewStatus, string> = {
  unreviewed: 'Unreviewed',
  reviewed: 'Reviewed',
  needs_attention: 'Needs attention',
};

export const CHANGE_LETTER: Record<ChangeType, string> = {
  added: 'A',
  deleted: 'D',
  modified: 'M',
  renamed: 'R',
  copied: 'C',
};

export const CHANGE_SEVERITY: Record<ChangeType, 'success' | 'danger' | 'info' | 'warn' | 'secondary'> = {
  added: 'success',
  deleted: 'danger',
  modified: 'info',
  renamed: 'warn',
  copied: 'secondary',
};
