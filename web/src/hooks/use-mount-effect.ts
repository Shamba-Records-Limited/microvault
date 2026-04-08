/**
 * Explicit "run on mount only" hook — the sanctioned escape hatch from the
 * project's no-`useEffect` rule.
 * @module hooks/use-mount-effect
 */
import { useEffect } from "react";

/**
 * Runs an effect exactly once on mount, with optional cleanup on unmount.
 * @param effect - Setup function; may return a cleanup function
 * @remarks Use for one-time external system sync: DOM integration, SSE
 * streams, third-party subscriptions. Wraps `useEffect` with an empty
 * dependency array so intent is explicit at the call site and ESLint's
 * `no-restricted-syntax` ban on direct `useEffect` stays green.
 */
export function useMountEffect(effect: () => void | (() => void)) {
  // eslint-disable-next-line no-restricted-syntax
  useEffect(effect, []);
}
