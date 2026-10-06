/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** The agenty server's URL, such as https://agenty.example.com. */
  readonly VITE_AGENTY_API_URL?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
