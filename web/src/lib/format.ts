const dateTime = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

/** A timestamp from the API, in the user's locale and time zone. */
export function formatTime(iso: string): string {
  return dateTime.format(new Date(iso));
}

/** The time from one timestamp to another, such as "1m 30s". */
export function formatDuration(from: string, to: string): string {
  const seconds = Math.floor((Date.parse(to) - Date.parse(from)) / 1000);
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
