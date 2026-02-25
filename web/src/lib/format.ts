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
