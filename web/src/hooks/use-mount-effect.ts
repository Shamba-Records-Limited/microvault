import { useEffect } from "react";

/**
 * Runs an effect exactly once on mount, with optional cleanup on unmount.
 * Wraps `useEffect` with an empty dependency array to make intent explicit.
 *
 * Use for one-time external system sync: DOM integration, SSE streams,
 * third-party subscriptions.
 */
export function useMountEffect(effect: () => void | (() => void)) {
  // eslint-disable-next-line no-restricted-syntax
  useEffect(effect, []);
}
