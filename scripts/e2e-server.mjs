import { spawn, spawnSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
const temporary = mkdtempSync(join(tmpdir(), 'whatnow-e2e-'));
const binary = join(temporary, process.platform === 'win32' ? 'whatnow.exe' : 'whatnow');
const cleanup = () => rmSync(temporary, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
// Never reuse an untagged server: the e2e build replaces the real model and keychain.
const build = spawnSync('go', ['build', '-tags', 'e2e', '-o', binary, './cmd/whatnow'], {
	cwd: root, stdio: 'inherit'
});
if (build.error || build.status !== 0) {
	cleanup();
	if (build.error) console.error(build.error.message);
	process.exit(build.status ?? 1);
}
const server = spawn(binary, ['serve', '-addr', '127.0.0.1:7788', '-db', join(temporary, 'whatnow.db')], {
	cwd: root, stdio: 'inherit'
});
let stopping = false;
for (const signal of ['SIGINT', 'SIGTERM']) {
	process.on(signal, () => {
		stopping = true;
		// Only terminate the child we own, never an entire shell/process group.
		server.kill(signal);
	});
}
server.on('error', (error) => console.error(error.message));
server.on('close', (code) => {
	cleanup();
	process.exitCode = stopping ? 0 : (code ?? 1);
});
