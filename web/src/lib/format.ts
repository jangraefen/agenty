const dateTime = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

/** A timestamp from the API, in the user's locale and time zone. */
export function formatTime(iso: string): string {
  return dateTime.format(new Date(iso));
}

/** The time from one timestamp to another, such as "1m 30s". */
export function formatDuration(from: string, to: string): string {
  return formatSpan(Date.parse(to) - Date.parse(from));
}

/** The time left until a deadline at now, such as "1m 30s left", or "expired". */
export function formatRemaining(until: string, now: number): string {
  const left = Date.parse(until) - now;
  return left <= 0 ? "expired" : `${formatSpan(left)} left`;
}

type Usage = {
  input_tokens: number;
  output_tokens: number;
  cache_write_tokens: number;
  cache_read_tokens: number;
};

/**
 * Tokens of model calls, such as "12.3k tokens in, 1k out, 80% from the
 * cache": the input counts what was read from and written to the cache too.
 */
export function formatUsage(usage: Usage): string {
  const input = usage.input_tokens + usage.cache_write_tokens + usage.cache_read_tokens;
  const parts = [`${formatCount(input)} tokens in`, `${formatCount(usage.output_tokens)} out`];
  if (usage.cache_read_tokens > 0) {
    // Rounded down: all from the cache only when it all was.
    parts.push(`${Math.floor((100 * usage.cache_read_tokens) / input)}% from the cache`);
  }
  return parts.join(", ");
}

/** The exact tokens of model calls, for a tooltip. */
export function formatUsageExactly(usage: Usage): string {
  const n = (count: number) => count.toLocaleString("en-US");
  return `${n(usage.input_tokens)} input tokens, ${n(usage.cache_write_tokens)} written to and ${n(usage.cache_read_tokens)} read from the cache; ${n(usage.output_tokens)} output tokens`;
}

// A count, shortened to thousands or millions, in English like the rest of
// the app.
function formatCount(n: number): string {
  // Rounded first, so 999,950 is 1M, not 1000k.
  const thousands = Number((n / 1_000).toFixed(1));
  if (thousands >= 1_000) {
    return `${Number((n / 1_000_000).toFixed(1))}M`;
  }
  if (n >= 1_000) {
    return `${thousands}k`;
  }
  return String(n);
}

// A span of milliseconds, in English like the rest of the app.
function formatSpan(ms: number): string {
  const seconds = Math.floor(ms / 1000);
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (hours > 0) {
    return `${hours}h ${minutes}m`;
  }
  if (minutes > 0) {
    return `${minutes}m ${seconds % 60}s`;
  }
  return `${seconds}s`;
}
