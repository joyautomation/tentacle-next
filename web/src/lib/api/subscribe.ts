// SSE subscription helper for tentacle Go backend

const API_BASE = '/api/v1';

/**
 * Subscribe to a Server-Sent Events endpoint.
 * Returns a cleanup function to close the connection.
 */
export function subscribe<T = unknown>(
  path: string,
  onData: (data: T) => void,
  onError?: (error: Error) => void
): () => void {
  const url = `${API_BASE}${path}`;
  const eventSource = new EventSource(url);

  eventSource.onmessage = (event) => {
    try {
      const parsed = JSON.parse(event.data);
      onData(parsed as T);
    } catch (e) {
      onError?.(e instanceof Error ? e : new Error('Parse error'));
    }
  };

  eventSource.onerror = () => {
    onError?.(new Error('SSE connection lost'));
  };

  return () => {
    eventSource.close();
  };
}
