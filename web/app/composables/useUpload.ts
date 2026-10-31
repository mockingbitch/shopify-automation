import { ApiError } from '~/utils/api-error'

/**
 * useUpload posts multipart forms (openapi-fetch only sends JSON). It sends
 * the CSRF token like useApi and returns the parsed `data` field.
 */
export function useUpload() {
  const session = useSessionStore()
  return async function upload<T>(path: string, file: File, fields: Record<string, string | undefined> = {}): Promise<T> {
    const form = new FormData()
    form.append('file', file)
    for (const [k, v] of Object.entries(fields)) if (v) form.append(k, v)
    let res: Response
    try {
      res = await fetch(`/api/v1${path}`, {
        method: 'POST', body: form, credentials: 'same-origin', headers: { 'X-CSRF-Token': session.csrfToken },
      })
    } catch {
      throw toApiError(undefined)
    }
    const body = await res.json().catch(() => ({}))
    if (!res.ok) {
      throw new ApiError({
        type: body.type ?? 'about:blank', title: body.title ?? res.statusText, status: res.status,
        code: body.code ?? 'unknown_error', detail: body.detail ?? 'Tải tệp lên không thành công. Bạn hãy thử lại.', errors: body.errors,
      })
    }
    return body.data as T
  }
}
