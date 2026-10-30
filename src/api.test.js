import { describe, expect, it, vi } from 'vitest';
import { APIError, createAPI } from './api';

function response(body, status = 200, headers = {}) { return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', ...headers } }); }

describe('Go API transport', () => {
  it('uses encoded server filters and a runtime bearer token', async () => {
    const fetcher = vi.fn().mockResolvedValue(response({ data: [], total: 0 }));
    let token = 'first-token';
    const api = createAPI({ baseURL: '/api/v1/', getToken: () => token, fetcher });
    await api.listExperiments({ q: 'drag & range', status: 'needs_review', limit: 20 });
    expect(fetcher.mock.calls[0][0]).toBe('/api/v1/experiments?q=drag+%26+range&status=needs_review&limit=20');
    expect(fetcher.mock.calls[0][1].headers.Authorization).toBe('Bearer first-token');
    token = 'second-token';
    fetcher.mockResolvedValue(response({ model_provider: 'disabled' }));
    await api.settings();
    expect(fetcher.mock.calls[1][1].headers.Authorization).toBe('Bearer second-token');
  });
  it('sends JSON and exposes backend validation errors without pretending success', async () => {
    const fetcher = vi.fn().mockResolvedValue(response({ error: { code: 'invalid_plan', message: 'Include a zero-drag control' } }, 422));
    const api = createAPI({ fetcher });
    await expect(api.savePlan('exp_test', { adapter: 'physics.projectile' })).rejects.toMatchObject({ status: 422, code: 'invalid_plan', message: 'Include a zero-drag control' });
    expect(fetcher.mock.calls[0][1].method).toBe('PUT');
    expect(fetcher.mock.calls[0][1].headers['Content-Type']).toBe('application/json');
  });
  it('uploads actual multipart files without overriding the browser boundary', async () => {
    const fetcher = vi.fn().mockResolvedValue(response({ id: 'ds_test' }));
    const api = createAPI({ fetcher });
    const file = new File(['group,value\nA,1'], 'groups.csv', { type: 'text/csv' });
    await api.uploadDataset(file);
    const options = fetcher.mock.calls[0][1];
    expect(options.body).toBeInstanceOf(FormData);
    expect(options.body.get('file').name).toBe('groups.csv');
    expect(options.headers['Content-Type']).toBeUndefined();
  });
  it('authenticates binary artifact downloads and preserves nested attempt paths', async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response('report content', { headers: { 'Content-Type': 'text/plain' } }));
    const api = createAPI({ getToken: () => 'token', fetcher });
    const result = await api.downloadArtifact('exp_test', 'attempt-001/report.md');
    expect(fetcher.mock.calls[0][0]).toBe('/api/v1/experiments/exp_test/artifacts/attempt-001/report.md');
    expect(fetcher.mock.calls[0][1].headers.Authorization).toBe('Bearer token');
    expect(result.blob.size).toBe(14);
  });
  it('reports offline connections, proxy errors, and auth failures', async () => {
    const api = createAPI({ fetcher: vi.fn().mockRejectedValue(new TypeError('fetch failed')) });
    await expect(api.dashboard()).rejects.toBeInstanceOf(APIError);
    const proxy = createAPI({ fetcher: vi.fn().mockResolvedValue(new Response('<html>not the API</html>', { status: 200 })) });
    await expect(proxy.dashboard()).rejects.toMatchObject({ code: 'invalid_response' });
    const unauthorized = createAPI({ fetcher: vi.fn().mockResolvedValue(response({ error: { code: 'unauthorized', message: 'A valid bearer token is required' } }, 401)) });
    await expect(unauthorized.settings()).rejects.toMatchObject({ status: 401, code: 'unauthorized' });
  });
  it('accepts a 204 delete response and preserves cancellation', async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    await expect(createAPI({ fetcher }).deleteExperiment('exp_test')).resolves.toBeNull();
    const aborted = new DOMException('Aborted', 'AbortError');
    const api = createAPI({ fetcher: vi.fn().mockRejectedValue(aborted) });
    await expect(api.getExperiment('exp_test')).rejects.toBe(aborted);
  });
});
