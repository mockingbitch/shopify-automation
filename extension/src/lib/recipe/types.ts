export const MARKETS = ['VN', 'SG', 'MY', 'TH', 'PH', 'ID', 'TW'] as const;
export type Market = (typeof MARKETS)[number];

export const ROLES = [
  'button', 'link', 'textbox', 'combobox', 'checkbox', 'radio', 'tab', 'menuitem', 'option', 'heading', 'img', 'dialog',
] as const;
export type Role = (typeof ROLES)[number];

export const ACTIONS = [
  'goto', 'wait_for', 'click', 'fill', 'select', 'upload', 'expect', 'assert_shop_id', 'screenshot',
] as const;
export type ActionName = (typeof ACTIONS)[number];

/** Actions that change the page. assert_shop_id must pass before the first one. */
export const WRITE_ACTIONS: ReadonlySet<ActionName> = new Set(['click', 'fill', 'select', 'upload']);

export interface Target {
  role?: Role;
  name_any?: string[];
  selector?: string;
  text_any?: string[];
}

export type Step =
  | { goto: { url: string }; timeout_ms?: number }
  | { wait_for: Target; timeout_ms?: number }
  | { click: Target; timeout_ms?: number }
  | { fill: { target: Target; value: string }; timeout_ms?: number }
  | { select: { target: Target; value: string }; timeout_ms?: number }
  | { upload: { target: Target; files: string }; timeout_ms?: number }
  | { expect: { text_any: string[] } | { target: Target }; timeout_ms?: number }
  | { assert_shop_id: { target: Target; pattern: string }; timeout_ms?: number }
  | { screenshot: { name: string }; timeout_ms?: number };

export type StopOn = { url_matches: string } | { text_any: string[] } | { selector: string };

export interface RecipeInput {
  type: 'text' | 'files';
  required?: boolean;
}

export interface Recipe {
  capability: string;
  version: number;
  market: Market;
  start_url: string;
  allowed_origins: string[];
  pace_ms?: number;
  inputs: Record<string, RecipeInput>;
  steps: Step[];
  verify?: Step[];
  stop_on: StopOn[];
}

export function actionOf(step: Step): ActionName {
  for (const a of ACTIONS) if (a in step) return a;
  throw new Error('step has no action');
}

export const DEFAULT_PACE_MS = 800;
