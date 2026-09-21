// The whole app — both the BBS user portal at / and the sysop admin
// panel at /admin — is a static SPA served by cmd/web (see
// internal/web): no SvelteKit server exists at runtime, so
// server-side rendering is disabled and the static build's fallback
// index.html handles all client-side routes.
export const ssr = false;
