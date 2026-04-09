/**
 * Shared freshness/staleness utilities for live variable displays.
 *
 * These functions power the freshness dot color (green → gray fade),
 * glow effect, and age labels across all service pages.
 */

const FADE_DURATION_SECONDS = 300;

/** Parse an ISO string or epoch-ms number to epoch-ms. Returns null on failure. */
export function toEpochMs(value: string | number | null | undefined): number | null {
  if (value == null) return null;
  if (typeof value === 'number') return value;
  const ms = Date.parse(value);
  return Number.isNaN(ms) ? null : ms;
}

/**
 * Return a CSS color for a freshness dot: bright green when just updated,
 * fading linearly to gray over FADE_DURATION_SECONDS.
 * When quality is "bad", always returns red regardless of age.
 */
export function getFreshnessColor(
  timestamp: number | null | undefined,
  now: number,
  quality?: string,
): string {
  if (quality === 'bad') return 'rgb(239, 68, 68)'; // red
  if (!timestamp) return 'rgb(156, 163, 175)'; // gray
  const ageSeconds = (now - timestamp) / 1000;
  if (ageSeconds <= 0) return 'rgb(34, 197, 94)'; // green
  if (ageSeconds >= FADE_DURATION_SECONDS) return 'rgb(156, 163, 175)'; // gray
  const t = 1 - ageSeconds / FADE_DURATION_SECONDS;
  const r = Math.round(156 + (34 - 156) * t);
  const g = Math.round(163 + (197 - 163) * t);
  const b = Math.round(175 + (94 - 175) * t);
  return `rgb(${r}, ${g}, ${b})`;
}

/** Return a CSS box-shadow glow for recently-updated values. */
export function getGlowStyle(
  timestamp: number | null | undefined,
  now: number,
): string {
  if (!timestamp) return 'none';
  const ageSeconds = (now - timestamp) / 1000;
  const opacity =
    ageSeconds <= 0 ? 1 : ageSeconds >= FADE_DURATION_SECONDS ? 0 : 1 - ageSeconds / FADE_DURATION_SECONDS;
  if (opacity < 0.5) return 'none';
  const glowOpacity = (opacity - 0.5) * 2;
  return `0 0 ${6 + glowOpacity * 4}px rgba(34, 197, 94, ${glowOpacity * 0.5})`;
}

/** Human-readable age for tooltip (e.g. "45s ago", "3m ago"). */
export function formatAge(
  timestamp: number | null | undefined,
  now: number,
): string {
  if (!timestamp) return 'No data';
  const ageMs = Math.max(0, now - timestamp);
  const ageSeconds = Math.floor(ageMs / 1000);
  if (ageSeconds < 60) return `${ageSeconds}s ago`;
  const ageMinutes = Math.floor(ageSeconds / 60);
  if (ageMinutes < 60) return `${ageMinutes}m ago`;
  const ageHours = Math.floor(ageMinutes / 60);
  if (ageHours < 24) return `${ageHours}h ago`;
  const ageDays = Math.floor(ageHours / 24);
  return `${ageDays}d ago`;
}

/** Short inline age label (e.g. "1s", "3m"). Empty string when no data. */
export function formatAgeShort(
  timestamp: number | null | undefined,
  now: number,
): string {
  if (!timestamp) return '';
  const ageMs = Math.max(0, now - timestamp);
  const ageSeconds = Math.floor(ageMs / 1000);
  if (ageSeconds < 60) return `${ageSeconds}s`;
  const ageMinutes = Math.floor(ageSeconds / 60);
  if (ageMinutes < 60) return `${ageMinutes}m`;
  const ageHours = Math.floor(ageMinutes / 60);
  if (ageHours < 24) return `${ageHours}h`;
  const ageDays = Math.floor(ageHours / 24);
  return `${ageDays}d`;
}
