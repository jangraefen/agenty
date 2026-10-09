import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

/**
 * shadcn/ui's class-name helper, used by the copied ui components and
 * others: clsx joins conditional class names, then tailwind-merge drops the
 * Tailwind classes a later one overrides, so a className passed to a
 * component wins over its defaults.
 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
