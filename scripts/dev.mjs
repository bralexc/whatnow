import { spawn, spawnSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
const temporary = mkdtempSync(join(tmpdir(), 'whatnow-dev-'));
const binary = join(temporary, process.platform === 'win32' ? 'whatnow.exe' : 'whatnow');
const cleanup = () => rmSync(temporary, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
// Build first, so the process we manage is the server itself, not a go-run wrapper.
const build = spawnSync('go', ['build', '-o', binary, './cmd/whatnow'], { cwd: root, stdio: 'inherit' });
if (build.error || build.status !== 0) {
	cleanup();
	if (build.error) console.error(build.error.message);
	process.exit(build.status ?? 1);
}

const children = new Set();
let stopping = false;
let killTimer;
function stop(code) {
	if (stopping) return;
	stopping = true;
	process.exitCode = code;
	for (const child of children) child.kill('SIGTERM');
	killTimer = setTimeout(() => {
		for (const child of children) child.kill('SIGKILL');
	}, 7000);
	killTimer.unref();
}
function launch(command, args, cwd) {
	const child = spawn(command, args, { cwd, stdio: 'inherit' });
	children.add(child);
	child.on('error', (error) => {
		console.error(error.message);
		stop(1);
	});
	child.on('close', (code) => {
		children.delete(child);
		stop(code ?? 1);
		if (children.size === 0) {
			clearTimeout(killTimer);
			cleanup();
		}
	});
}
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => stop(0));
launch(binary, ['serve', ...process.argv.slice(2)], root);
launch(process.execPath, [join(root, 'web/node_modules/vite/bin/vite.js'), 'dev'], join(root, 'web'));
