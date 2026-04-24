// Typed REST helpers for the HMI module.

import { api, apiPost, apiPut, apiDelete, type ApiResult } from './client';
import type {
  HmiAppConfig,
  HmiComponentConfig,
  HmiScreenConfig,
  HmiUdtTemplate,
} from '$lib/types/hmi';

export function listHmiApps(): Promise<ApiResult<HmiAppConfig[]>> {
  return api<HmiAppConfig[]>('/hmi/apps');
}

export function getHmiApp(appId: string): Promise<ApiResult<HmiAppConfig>> {
  return api<HmiAppConfig>(`/hmi/apps/${encodeURIComponent(appId)}`);
}

export function createHmiApp(input: {
  appId?: string;
  name: string;
  description?: string;
}): Promise<ApiResult<HmiAppConfig>> {
  return apiPost<HmiAppConfig>('/hmi/apps', input);
}

export function deleteHmiApp(appId: string): Promise<ApiResult<unknown>> {
  return apiDelete(`/hmi/apps/${encodeURIComponent(appId)}`);
}

/** Replace the entire app config (used for app-wide settings like classes). */
export function putHmiApp(
  appId: string,
  app: HmiAppConfig,
): Promise<ApiResult<HmiAppConfig>> {
  return apiPut<HmiAppConfig>(`/hmi/apps/${encodeURIComponent(appId)}`, app);
}

export function createHmiScreen(
  appId: string,
  input: { screenId?: string; name: string }
): Promise<ApiResult<HmiScreenConfig>> {
  return apiPost<HmiScreenConfig>(
    `/hmi/apps/${encodeURIComponent(appId)}/screens`,
    input
  );
}

export function putHmiScreen(
  appId: string,
  screenId: string,
  screen: Omit<HmiScreenConfig, 'screenId'>
): Promise<ApiResult<HmiScreenConfig>> {
  return apiPut<HmiScreenConfig>(
    `/hmi/apps/${encodeURIComponent(appId)}/screens/${encodeURIComponent(screenId)}`,
    screen
  );
}

export function deleteHmiScreen(
  appId: string,
  screenId: string
): Promise<ApiResult<unknown>> {
  return apiDelete(
    `/hmi/apps/${encodeURIComponent(appId)}/screens/${encodeURIComponent(screenId)}`
  );
}

export function createHmiComponent(
  appId: string,
  input: { componentId?: string; name: string; udtTemplate?: string }
): Promise<ApiResult<HmiComponentConfig>> {
  return apiPost<HmiComponentConfig>(
    `/hmi/apps/${encodeURIComponent(appId)}/components`,
    input
  );
}

export function putHmiComponent(
  appId: string,
  componentId: string,
  component: HmiComponentConfig
): Promise<ApiResult<HmiComponentConfig>> {
  return apiPut<HmiComponentConfig>(
    `/hmi/apps/${encodeURIComponent(appId)}/components/${encodeURIComponent(componentId)}`,
    component
  );
}

export function deleteHmiComponent(
  appId: string,
  componentId: string
): Promise<ApiResult<unknown>> {
  return apiDelete(
    `/hmi/apps/${encodeURIComponent(appId)}/components/${encodeURIComponent(componentId)}`
  );
}

export function listHmiUdts(): Promise<ApiResult<HmiUdtTemplate[]>> {
  return api<HmiUdtTemplate[]>('/hmi/udts');
}
