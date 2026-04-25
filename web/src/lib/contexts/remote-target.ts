import { getContext, setContext } from 'svelte';

const KEY = Symbol('remote-target');

export interface RemoteTargetContext {
  target: string | null;
  isRemote: boolean;
  targetSuffix: string;
}

export function setRemoteTargetContext(getState: () => RemoteTargetContext): void {
  setContext(KEY, getState);
}

// getRemoteTarget returns the live remote-target state from the nearest
// service layout. Pages call this at the top level of <script> and read the
// fields reactively (the returned getter is a $derived snapshot fn).
export function getRemoteTarget(): () => RemoteTargetContext {
  const fn = getContext<() => RemoteTargetContext>(KEY);
  return fn ?? (() => ({ target: null, isRemote: false, targetSuffix: '' }));
}
