/// <reference types="vite/client" />

interface ImportMetaEnv {
  /**
   * The agenty server's URL, such as https://agenty.example.com, without a
   * trailing slash; vite.config.ts sets its default.
   */
  readonly VITE_AGENTY_API_URL: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
