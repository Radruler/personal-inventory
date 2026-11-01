import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vitest/config';

// `make dev` runs `inv serve` and vite in the same container; vite proxies
// API and media requests to it.
const api = process.env.INV_API ?? 'http://127.0.0.1:8480';

export default defineConfig({
  plugins: [sveltekit()],
  server: {
    port: 5173,
    proxy: { '/api': api, '/media': api }
  },
  test: {
    include: ['src/**/*.test.ts'],
    environment: 'node'
  }
});
