/**
 * Number formatting helpers for currency, percentages, and amount inputs.
 * @module lib/format
 */

/**
 * Formats a number as a compact USD string.
 * @param value - Dollar amount (already in major units, not stroops)
 * @returns Compact label such as `$1.50M`, `$3.00K`, or `$42.00`
 * @remarks Compact suffixes kick in at 1K and 1M; values below 1K render with
 * exactly two decimals so the dashboard column widths stay stable.
 */
export function formatCurrency(value: number): string {
  if (value >= 1_000_000) return `$${(value / 1_000_000).toFixed(2)}M`;
  if (value >= 1_000) return `$${(value / 1_000).toFixed(2)}K`;
  return `$${value.toFixed(2)}`;
}

/**
 * Formats a 0–100 percentage with two decimal places.
 * @param value - Percentage already in 0–100 range (not 0–1)
 * @returns String like `12.34%`
 */
export function formatPercent(value: number): string {
  return `${value.toFixed(2)}%`;
}

/**
 * Formats a number with thousand separators, trimming trailing zeros down to
 * a minimum of two decimal places.
 * @param value - Numeric value to format
 * @param decimals - Maximum decimal places to consider before trimming (default 4)
 * @returns String like `1,234.5678`, `1,234.50`, or `1,234.00`
 * @remarks Keeping at least two decimals prevents the layout from jumping
 * between integer and fractional displays as values change.
 */
export function formatNumber(value: number, decimals = 4): string {
  const parts = value.toFixed(decimals).split(".");
  parts[0] = parts[0].replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  // Trim trailing zeros from decimal part but keep at least 2
  if (parts[1]) {
    const trimmed = parts[1].replace(/0+$/, "");
    parts[1] = trimmed.length < 2 ? trimmed.padEnd(2, "0") : trimmed;
  }
  return parts.join(".");
}

/**
 * Formats a raw input string with thousand separators while preserving an
 * uninterrupted typing experience.
 * @param value - Whatever the user has typed so far, possibly already formatted
 * @returns Both the user-facing `display` string (with commas) and the
 * comma-stripped `raw` string suitable for `parseFloat`/`BigInt`
 * @remarks Trailing dots and partial decimals are passed through unchanged so
 * the cursor doesn't jump while the user is mid-typing a value like `1,234.`.
 * Non-numeric input is also passed through to let validation surface errors
 * higher up rather than silently dropping characters.
 */
export function formatInputValue(value: string): {
  display: string;
  raw: string;
} {
  // Strip existing commas to get raw value
  const raw = value.replace(/,/g, "");

  // Don't format if empty or just a decimal point
  if (!raw || raw === ".") return { display: raw, raw };

  // Validate it's a valid number-in-progress (allow trailing dot and incomplete decimals)
  if (!/^\d*\.?\d*$/.test(raw)) return { display: raw, raw };

  const parts = raw.split(".");
  parts[0] = parts[0].replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  return { display: parts.join("."), raw };
}
