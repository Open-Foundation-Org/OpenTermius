import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';

const allowedHostsEnv = process.env.ALLOWED_HOSTS?.trim();
const allowedHosts = allowedHostsEnv
  ? allowedHostsEnv.split(',').map((h) => h.trim()).filter(Boolean)
  : true;

const apiProxyTarget =
  process.env.API_PROXY_TARGET?.trim() ||
  `http://127.0.0.1:${process.env.SERVER_PORT?.trim() || '8080'}`;

const proxy = {
  '/api': {
    target: apiProxyTarget,
    changeOrigin: true,
    ws: true
  },
  '/health': {
    target: apiProxyTarget,
    changeOrigin: true
  }
};

export default defineConfig({
  plugins: [
    tailwindcss(),
    svelte()
  ],
  server: {
    port: 5174,
    allowedHosts,
    proxy
  },
  preview: {
    port: 5174,
    allowedHosts,
    proxy
  }
});
