import { defaultApiUrl } from "./api-url";

/** The agenty server's URL, without a trailing slash. */
export const apiUrl = (import.meta.env.VITE_AGENTY_API_URL ?? defaultApiUrl).replace(/\/+$/, "");
