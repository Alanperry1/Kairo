import { defineConfig, loadEnv } from 'vite';
import { authorizeProxyRequest } from './scripts/proxy-auth.mjs';

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const target = env.API_PROXY_TARGET || 'http://127.0.0.1:8080';
  const route = () => ({
    target,
    changeOrigin: true,
    configure(proxy) {
      proxy.on('proxyReq', (outgoing, incoming) => {
        authorizeProxyRequest(outgoing, incoming, env.API_TOKEN || '');
      });
    },
  });
  const proxy = { '/api': route(), '/healthz': route(), '/readyz': route() };
  return {
    server: { port: 5173, strictPort: true, proxy },
    preview: { proxy },
    test: { environment: 'jsdom', include: ['src/**/*.test.{js,jsx}'], restoreMocks: true },
  };
});
