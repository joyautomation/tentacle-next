// REST API client for tentacle Go backend

const API_BASE = '/api/v1';

export type ApiError = {
  error: string;
  status: number;
};

export type ApiResult<T> = { data: T; error?: never } | { data?: never; error: ApiError };

async function request<T>(path: string, options?: RequestInit): Promise<ApiResult<T>> {
  try {
    const response = await fetch(`${API_BASE}${path}`, {
      headers: { 'Content-Type': 'application/json', ...options?.headers },
      ...options,
    });
    if (!response.ok) {
      const text = await response.text().catch(() => response.statusText);
      let errorMessage = text;
      try {
        const json = JSON.parse(text);
        if (json.error) errorMessage = json.error;
      } catch { /* use raw text */ }
      return { error: { error: errorMessage, status: response.status } };
    }
    const data = await response.json();
    return { data };
  } catch (e) {
    return { error: { error: e instanceof Error ? e.message : 'Network error', status: 0 } };
  }
}

// GET helper
export function api<T>(path: string): Promise<ApiResult<T>> {
  return request<T>(path);
}

// PUT helper
export function apiPut<T>(path: string, body?: unknown): Promise<ApiResult<T>> {
  return request<T>(path, { method: 'PUT', body: body != null ? JSON.stringify(body) : undefined });
}

// POST helper
export function apiPost<T>(path: string, body?: unknown): Promise<ApiResult<T>> {
  return request<T>(path, { method: 'POST', body: body != null ? JSON.stringify(body) : undefined });
}

// DELETE helper
export function apiDelete<T>(path: string, body?: unknown): Promise<ApiResult<T>> {
  return request<T>(path, { method: 'DELETE', body: body != null ? JSON.stringify(body) : undefined });
}
