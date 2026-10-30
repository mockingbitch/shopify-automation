import type { Org, TaskSummary, User } from './api/types';
import type { RunState } from './runner/run';

export type Request =
  | { type: 'status' }
  | { type: 'pair'; code: string; deviceName: string }
  | { type: 'unpair' }
  | { type: 'refresh' }
  | { type: 'run'; taskId: string }
  | { type: 'stop' }
  | { type: 'continue' }
  | { type: 'dismiss' };

export interface AuthView {
  org: Org;
  user: User;
  deviceName: string;
}

export interface TasksView {
  items: TaskSummary[];
  fetchedAt: number | null;
  error?: string;
}

export interface Status {
  auth: AuthView | null;
  tasks: TasksView;
  run: RunState | null;
  notice?: string;
}

export type Reply<T = unknown> = { ok: true; data: T } | { ok: false; error: { code: string; message: string } };

export const MSG_CHANNEL = 'shopee-automation-popup' as const;
