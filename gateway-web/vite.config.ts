import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';

const allowedHostsEnv = process.env.ALLOWED_HOSTS?.trim();
const allowedHosts = allowedHostsEnv
  ? allowedHostsEnv.split(',').map((h) => h.trim()).filter(Boolean)
  : true;

export default defineConfig({
  plugins: [
    tailwindcss(),
    svelte()
  ],
  server: {
    port: 5174,
    allowedHosts
  },
  preview: {
    port: 5174,
    allowedHosts
  }
});
