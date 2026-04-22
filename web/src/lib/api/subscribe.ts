// SSE subscription helper for tentacle Go backend

const API_BASE = '/api/v1';

/**
 * Subscribe to a Server-Sent Events endpoint.
 * Listens for both unnamed messages and named events.
 * Returns a cleanup function to close the connection.
 */
export function subscribe<T = unknown>(
  path: string,
  onData: (data: T) => void,
  onError?: (error: Error) => void
): () => void {
  const url = `${API_BASE}${path}`;
  const eventSource = new EventSource(url);

  const handler = (event: MessageEvent) => {
    try {
      const parsed = JSON.parse(event.data);
      onData(parsed as T);
    } catch (e) {
      onError?.(e instanceof Error ? e : new Error('Parse error'));
    }
  };

  // Listen for unnamed events
  eventSource.onmessage = handler;

  // Also listen for common named events used by the backend
  for (const name of ['progress', 'variable', 'batch', 'log', 'network', 'nftables', 'metrics', 'try', 'test']) {
    eventSource.addEventListener(name, handler as EventListener);
  }

  eventSource.onerror = () => {
    onError?.(new Error('SSE connection lost'));
  };

  return () => {
    eventSource.close();
  };
}
