/// <reference types="vite/client" />

// Vite handles styles at build time. Keep the side-effect import explicit so
// TypeScript 7 can validate it without treating valid CSS imports as missing.
declare module "*.css" {}
