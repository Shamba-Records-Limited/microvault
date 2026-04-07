import { useEffect, useState } from "react";

/**
 * Returns `value` after it has been stable for `delay` ms. Used to avoid
 * firing per-keystroke RPC calls while the user is still typing. Genuine
 * use of `useEffect` (not derivable, not on-mount): we need a timer that
 * resets every time `value` changes and is cleaned up on unmount.
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
