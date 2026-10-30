import type { StopOn, Target } from '../recipe/types';

/** A file to put on an <input type=file>, carried as base64 because messages are JSON. */
export interface WireFile {
  name: string;
  type: string;
  b64: string;
}

/** Commands the service worker sends to the runner script in the tab. Already rendered. */
export type Command =
  | { op: 'ping' }
  | { op: 'stop_check'; stop_on: StopOn[] }
  | { op: 'wait_for'; target: Target; timeoutMs: number }
  | { op: 'click'; target: Target; timeoutMs: number }
  | { op: 'fill'; target: Target; value: string; timeoutMs: number }
  | { op: 'select'; target: Target; value: string; timeoutMs: number }
  | { op: 'upload'; target: Target; files: WireFile[]; timeoutMs: number }
  | { op: 'expect'; text_any?: string[]; target?: Target; timeoutMs: number }
  | { op: 'assert_shop_id'; target: Target; pattern: string; expected: string; timeoutMs: number };

export type CommandResult =
  | { ok: true; detail?: string; matched?: number; href?: string }
  | { ok: false; code: string; message: string };

export interface RunnerMessage {
  channel: 'shopee-automation-runner';
  command: Command;
}

export const RUNNER_CHANNEL = 'shopee-automation-runner' as const;
