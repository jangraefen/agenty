/**
 * The frontend's build-time configuration. The frontend is a static bundle
 * deployed apart from the server, so where the API lives is fixed when it
 * is built: vite.config.ts reads VITE_AGENTY_API_URL, defaults it to the
 * local server and strips a trailing slash, and the built Content-Security-
 * Policy allows connections to that origin alone.
 */

/** The agenty server's URL, without a trailing slash: client.ts's base URL. */
export const apiUrl = import.meta.env.VITE_AGENTY_API_URL;
