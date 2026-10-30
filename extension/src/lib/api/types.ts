/** RFC 9457 problem details as the API sends them. */
export interface Problem {
  type?: string;
  title?: string;
  status?: number;
  code?: string;
  detail?: string;
  request_id?: string;
  errors?: unknown;
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly problem: Problem,
  ) {
    super(problem.detail || problem.title || `${status} ${code}`);
    this.name = 'ApiError';
  }
}

export const isLeaseLost = (e: unknown): boolean => e instanceof ApiError && e.status === 409 && e.code === 'lease_lost';

export interface Device {
  id: string;
  name: string;
  [k: string]: unknown;
}
export interface Org {
  id: string;
  name: string;
  slug?: string;
  [k: string]: unknown;
}
export interface User {
  id: string;
  email: string;
  name?: string;
  [k: string]: unknown;
}

export interface PairResponse {
  token: string;
  expires_at: string;
  device: Device;
  org: Org;
  user: User;
}

export interface MeResponse {
  device: Device;
  org: Org;
  user: User;
}

export interface RotateResponse {
  token: string;
  expires_at: string;
}

export type TaskStatus = 'QUEUED' | 'RUNNING' | 'WAITING_USER' | 'SUCCEEDED' | 'FAILED' | 'CANCELLED';

export interface TaskShop {
  id: string;
  shopee_shop_id: string | number;
  name: string;
  region: string;
}

export interface TaskSummary {
  id: string;
  status: TaskStatus;
  capability: string;
  title: string;
  shop: TaskShop;
  preview?: unknown;
  created_at: string;
}

export interface InputFile {
  name: string;
  url: string;
  content_type: string;
  sha256?: string;
}

export type TaskInput = { type: 'text'; value: string } | { type: 'files'; files: InputFile[] };

export interface ClaimResponse {
  lease_token: string;
  lease_expires_at: string;
  recipe: { json: string; signature: string; key_id: string };
  inputs: Record<string, TaskInput>;
  task: { shopee_shop_id: string | number; region: string; capability: string };
}

export interface StepLog {
  index: number;
  action: string;
  outcome: 'ok' | 'failed' | 'skipped';
  duration_ms: number;
  error?: string;
}

export interface ResultBody {
  lease_token: string;
  status: 'SUCCEEDED' | 'FAILED';
  error_code?: string;
  message?: string;
  steps: StepLog[];
  screenshot_ids?: string[];
}
