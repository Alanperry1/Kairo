// Keep Go build outputs and temporary files on the same drive as this checkout.
import { mkdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { join } from 'node:path';
import { spawn } from 'node:child_process';
const root = fileURLToPath(new URL('../', import.meta.url));
const cache = join(root, 'backend/data/.build-cache');
const binary = join(root, 'backend/data/.local/bin/kairo-api');
const directories = { GOCACHE: join(cache, 'go-cache'), GOMODCACHE: join(cache, 'go-mod'), GOTMPDIR: join(cache, 'tmp'), TMPDIR: join(cache, 'tmp') };
for (const path of [...Object.values(directories), join(root, 'backend/data/.local/bin')]) mkdirSync(path, { recursive: true });
const build = process.argv.includes('--build');
const migrate = process.argv.includes('--migrate-storage');
const child = spawn(process.env.GO_BIN || 'go', build ? ['build', '-buildvcs=false', '-o', binary, './cmd/server'] : ['run', '-buildvcs=false', migrate ? './cmd/migrate-storage' : './cmd/server', ...process.argv.slice(2).filter((arg)=>arg!=='--migrate-storage')], {
  cwd: join(root, 'backend'), env: { ...process.env, ...directories }, stdio: 'inherit',
});
child.on('error', () => { console.error('Could not start Go. Install Go or set GO_BIN to its executable.'); process.exitCode = 1; });
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => child.kill(signal));
child.on('exit', (code, signal) => { process.exitCode = code ?? (signal ? 1 : 0); });
