/**
 * Tailwind class-name helper used by every component in the project.
 * @module lib/utils
 */
import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/**
 * Merges Tailwind class names, resolving conflicts in favour of the last one.
 * @param inputs - Any mix of strings, arrays, or conditional `clsx` objects
 * @returns A single class string with conflicting Tailwind utilities deduped
 * @remarks Uses `tailwind-merge` so that e.g. `cn("px-2", "px-4")` yields
 * `"px-4"` rather than both classes — consumers can safely override base
 * styles by appending overrides at the end.
 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
