const dateTime = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });
// Hours and minutes, or minutes and seconds, the smaller unit even when 0.
const hoursMinutes = new Intl.DurationFormat(undefined, {
  style: "narrow",
  minutesDisplay: "always",
});
const minutesSeconds = new Intl.DurationFormat(undefined, {
  style: "narrow",
  secondsDisplay: "always",
});

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

// A span of milliseconds, in the user's locale.
function formatSpan(ms: number): string {
  const seconds = Math.floor(ms / 1000);
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return hours > 0
    ? hoursMinutes.format({ hours, minutes })
    : minutesSeconds.format({ minutes, seconds: seconds % 60 });
}
