/**
 * Debounce hook for rate-limiting fast-changing values (typed input, etc).
 * @module hooks/use-debounced-value
 */
import { useEffect, useState } from "react";

/**
 * Returns `value` after it has been stable for `delay` ms.
 * @param value - The fast-changing source value
 * @param delay - Stability window in milliseconds
 * @returns The most recent value that has been stable for `delay` ms
 * @remarks Genuine use of `useEffect` (not derivable, not on-mount): we need a
 * timer that resets every time `value` changes and cleans up on unmount. The
 * `no-restricted-syntax` disable comment is intentional — this is the one
 * place `useMountEffect` cannot cover.
 */
export function useDebouncedValue<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value);

  // eslint-disable-next-line no-restricted-syntax
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(id);
  }, [value, delay]);

  return debounced;
}
