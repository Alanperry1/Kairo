import { describe, expect, it, vi } from 'vitest';
import { authorizeProxyRequest } from '../scripts/proxy-auth.mjs';
const incoming = (headers = {}) => ({ headers: { host: 'localhost:5173', ...headers }, socket: {} });
const outgoing = () => ({ setHeader: vi.fn(), removeHeader: vi.fn() });
describe('server-side env token proxy', () => {
  it('attaches the env token for local requests without exposing it to the client', () => {
    const out = outgoing();
    authorizeProxyRequest(out, incoming(), 'test-only-token');
    expect(out.setHeader).toHaveBeenCalledWith('authorization', 'Bearer test-only-token');
  });
  it('normalizes same-origin browser POSTs and uses the env token', () => {
    const out = outgoing();
    authorizeProxyRequest(out, incoming({ origin: 'http://localhost:5173', 'sec-fetch-site': 'same-origin' }), 'test-token');
    expect(out.removeHeader).toHaveBeenCalledWith('origin');
    expect(out.setHeader).toHaveBeenCalledWith('authorization', 'Bearer test-token');
  });
  it('does not grant env credentials to foreign origins or cross-site requests', () => {
    for (const headers of [{ origin: 'https://foreign.example' }, { 'sec-fetch-site': 'cross-site' }, { origin: 'http://localhost:9999' }, { origin: 'invalid' }]) {
      const out = outgoing();
      authorizeProxyRequest(out, incoming(headers), 'test-token');
      expect(out.setHeader).not.toHaveBeenCalled();
    }
  });
  it('preserves manual authentication when no env token is configured', () => {
    const out = outgoing();
    authorizeProxyRequest(out, incoming(), '');
    expect(out.setHeader).not.toHaveBeenCalled();
  });
});
