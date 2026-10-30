import { ApiError, type ClaimResponse, type MeResponse, type PairResponse, type Problem, type ResultBody, type RotateResponse, type TaskSummary } from './types';

export interface ClientOptions {
  baseUrl: string;
  getToken: () => Promise<string | null>;
  /** Called on 401 device_unauthorized: the token is no longer valid. */
  onUnauthorized: () => Promise<void>;
  fetch?: typeof fetch;
}

async function readProblem(res: Response): Promise<Problem> {
  const text = await res.text().catch(() => '');
  try {
    const p = JSON.parse(text) as Problem;
    if (p && typeof p === 'object') return p;
  } catch {
    /* not JSON */
  }
  return { status: res.status, title: res.statusText || `HTTP ${res.status}`, detail: text.slice(0, 200) };
}

/** Client for /api/v1/ext. Every call except pair sends the device token. */
export class ExtApi {
  private readonly f: typeof fetch;

  constructor(private readonly opts: ClientOptions) {
    this.f = opts.fetch ?? ((...a) => fetch(...a));
  }

  private async request<T>(method: string, path: string, body?: unknown, auth = true): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (auth) {
      const token = await this.opts.getToken();
      if (!token) throw new ApiError(401, 'device_unauthorized', { title: 'Not paired', code: 'device_unauthorized' });
      headers.Authorization = `Bearer ${token}`;
    }
    let payload: BodyInit | undefined;
    if (body instanceof FormData) payload = body;
    else if (body !== undefined) {
      headers['Content-Type'] = 'application/json';
      payload = JSON.stringify(body);
    }
    let res: Response;
    try {
      res = await this.f(`${this.opts.baseUrl}${path}`, { method, headers, body: payload, credentials: 'omit', cache: 'no-store' });
    } catch (e) {
      throw new ApiError(0, 'network_error', { title: 'Cannot reach the server', detail: String((e as Error)?.message ?? e) });
    }
    if (!res.ok) {
      const p = await readProblem(res);
      const code = p.code ?? (res.status === 401 ? 'device_unauthorized' : `http_${res.status}`);
      if (res.status === 401 && auth && code === 'device_unauthorized') await this.opts.onUnauthorized();
      throw new ApiError(res.status, code, p);
    }
    if (res.status === 204) return undefined as T;
    const text = await res.text();
    return (text ? JSON.parse(text) : undefined) as T;
  }

  pair(code: string, deviceName: string, extensionVersion: string): Promise<PairResponse> {
    return this.request('POST', '/pair', { code, device_name: deviceName, extension_version: extensionVersion }, false);
  }
  me(): Promise<MeResponse> {
    return this.request('GET', '/me');
  }
  unpair(): Promise<void> {
    return this.request('DELETE', '/device');
  }
  rotate(): Promise<RotateResponse> {
    return this.request('POST', '/token/rotate');
  }
  async tasks(): Promise<TaskSummary[]> {
    const r = await this.request<TaskSummary[] | { data: TaskSummary[] }>('GET', '/tasks');
    return Array.isArray(r) ? r : (r?.data ?? []);
  }
  claim(id: string): Promise<ClaimResponse> {
    return this.request('POST', `/tasks/${encodeURIComponent(id)}/claim`);
  }
  heartbeat(id: string, leaseToken: string, state: 'RUNNING' | 'WAITING_USER', message?: string): Promise<unknown> {
    return this.request('POST', `/tasks/${encodeURIComponent(id)}/heartbeat`, { lease_token: leaseToken, state, ...(message ? { message } : {}) });
  }
  async screenshot(id: string, leaseToken: string, png: Blob, name: string): Promise<string> {
    const form = new FormData();
    form.append('lease_token', leaseToken);
    form.append('file', png, `${name}.png`);
    const r = await this.request<{ id: string }>('POST', `/tasks/${encodeURIComponent(id)}/screenshots`, form);
    return r.id;
  }
  result(id: string, body: ResultBody): Promise<void> {
    return this.request('POST', `/tasks/${encodeURIComponent(id)}/result`, body);
  }
  release(id: string, leaseToken: string): Promise<void> {
    return this.request('POST', `/tasks/${encodeURIComponent(id)}/release`, { lease_token: leaseToken });
  }
}
