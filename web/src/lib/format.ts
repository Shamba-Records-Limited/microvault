/** Format a number as a compact USD string (e.g. `$1.50M`, `$3.00K`, `$42.00`). */
export function formatCurrency(value: number): string {
  if (value >= 1_000_000) return `$${(value / 1_000_000).toFixed(2)}M`;
  if (value >= 1_000) return `$${(value / 1_000).toFixed(2)}K`;
  return `$${value.toFixed(2)}`;
}

/** Format a number as a percentage string with two decimal places. */
export function formatPercent(value: number): string {
  return `${value.toFixed(2)}%`;
}

/** Format a number with thousand separators and up to `decimals` decimal places (e.g. `1,234.5678`). */
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
 * Format a raw input string with thousand separators while preserving the editing experience.
 * Returns the formatted display string and the raw numeric string (without commas).
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
