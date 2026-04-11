export interface CommitEntry {
  sha: string;
  author: string;
  date: string;
  message: string;
}

export interface FieldDiff {
  path: string;
  action: 'added' | 'removed' | 'modified';
  oldValue?: unknown;
  newValue?: unknown;
}

export interface HistoryDiffChange {
  kind: string;
  name: string;
  action: 'added' | 'removed' | 'modified' | 'unchanged';
  fields?: FieldDiff[];
}

export interface DiffSummary {
  added: number;
  modified: number;
  removed: number;
  unchanged: number;
}

export interface HistoryDiffResult {
  fromSha: string;
  toSha: string;
  changes: HistoryDiffChange[];
  summary: DiffSummary;
}
