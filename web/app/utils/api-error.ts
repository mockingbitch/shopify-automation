import type { components } from '~/types/api'

export type Problem = components['schemas']['Problem']

/** ApiError wraps an RFC 9457 problem response from the API. */
export class ApiError extends Error {
  readonly problem: Problem

  constructor(problem: Problem) {
    super(problem.detail || problem.title)
    this.problem = problem
  }

  get status(): number {
    return this.problem.status
  }

  get code(): string {
    return this.problem.code
  }

  /** Field errors keyed by field name, for form display. */
  get fields(): Record<string, string> {
    const out: Record<string, string> = {}
    for (const e of this.problem.errors ?? []) out[e.field] = e.message
    return out
  }
}

/** toApiError turns any thrown value into an ApiError with a safe message. */
export function toApiError(e: unknown): ApiError {
  if (e instanceof ApiError) return e
  return new ApiError({
    type: 'about:blank',
    title: 'Lỗi kết nối',
    status: 0,
    code: 'network_error',
    detail: 'Không kết nối được tới máy chủ. Bạn hãy kiểm tra kết nối mạng rồi thử lại.',
  })
}

interface FetchResult<T> {
  data?: T
  error?: unknown
  response: Response
}

/** unwrap returns data or throws an ApiError for problem responses. */
export function unwrap<T>(res: FetchResult<T>): T {
  if (res.error !== undefined || !res.response.ok) {
    const p = res.error as Partial<Problem> | undefined
    throw new ApiError({
      type: p?.type ?? 'about:blank',
      title: p?.title ?? res.response.statusText,
      status: p?.status ?? res.response.status,
      code: p?.code ?? 'unknown_error',
      detail: p?.detail ?? 'Đã có lỗi xảy ra. Bạn hãy thử lại.',
      request_id: p?.request_id,
      errors: p?.errors,
    })
  }
  return res.data as T
}
