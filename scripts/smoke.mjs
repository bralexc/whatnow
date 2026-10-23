// Exercise the production binary from outside the checkout, with no external services.
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdtempSync, mkdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';

const binary = resolve(process.argv[2] ?? (process.platform === 'win32' ? 'dist/whatnow.exe' : 'dist/whatnow'));
const temporary = mkdtempSync(join(tmpdir(), 'whatnow-smoke-'));
const data = join(temporary, 'José & 日記 #1');
mkdirSync(data);
const database = join(data, 'whatnow.db');
const env = { ...process.env };
delete env.ANTHROPIC_API_KEY;
delete env.OPENAI_API_KEY;
delete env.WHATNOW_DEMO_CLOCK;
let child;
let exited;
let output = '';

async function stop() {
	if (!child) return;
	if (child.exitCode === null && child.signalCode === null) {
		// Windows has no POSIX signal delivery to child processes. Terminate only this
		// executable there; the Go tests exercise the graceful cancellation path.
		child.kill('SIGTERM');
		const timer = setTimeout(() => child.kill('SIGKILL'), 7000);
		try { await exited; } finally { clearTimeout(timer); }
	}
	child = undefined;
}

async function start() {
	output = '';
	child = spawn(binary, ['serve', '-addr', '127.0.0.1:0', '-db', database], {
		cwd: temporary, env, stdio: ['ignore', 'pipe', 'pipe']
	});
	let spawnError;
	child.on('error', (error) => { spawnError = error; });
	exited = once(child, 'close');
	// Attach a handler immediately so a spawn failure cannot become an unhandled rejection.
	exited.catch(() => {});
	for (const stream of [child.stdout, child.stderr]) {
		stream.on('data', (chunk) => { output += chunk.toString(); });
	}
	for (let attempt = 0; attempt < 100; attempt++) {
		if (spawnError) throw spawnError;
		if (child.exitCode !== null) throw new Error(`server exited: ${output}`);
		const url = output.match(/whatnow listening on (http:\/\/127\.0\.0\.1:\d+)/)?.[1];
		if (url) {
			try {
				const response = await fetch(`${url}/api/v1/settings`, { signal: AbortSignal.timeout(1000) });
				assert.equal(response.status, 200);
				const view = await response.json();
				assert.ok(view.settings && view.keys);
				return url;
			} catch { /* The listener is bound just before HTTP serving starts. */ }
		}
		await delay(100);
	}
	throw new Error(`server never became ready: ${output}`);
}

async function request(url, path, options) {
	const response = await fetch(url + path, { ...options, signal: AbortSignal.timeout(5000) });
	assert.ok(response.ok, `${path}: HTTP ${response.status}`);
	return response;
}

try {
	let url = await start();
	const html = await (await request(url, '/welcome')).text();
	const asset = html.match(/"(\/_app\/immutable\/entry\/[^" ]+\.js)"/)?.[1];
	assert.ok(asset, 'the executable must contain the built frontend, not the unbuilt placeholder');
	const js = await (await request(url, asset)).text();
	assert.ok(js.length > 0 && !js.startsWith('<!doctype'), 'embedded JavaScript must be served as an asset');
	const title = 'Windows standalone smoke test';
	await request(url, '/api/v1/tasks', {
		method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ title })
	});
	await stop();
	url = await start();
	const tasks = await (await request(url, '/api/v1/tasks')).json();
	assert.ok(tasks.some((task) => task.title === title), 'SQLite data must survive a process restart');
	console.log('Passed: embedded UI/assets, settings API, task creation, Unicode/spaced database path, restart persistence.');
} finally {
	await stop();
	rmSync(temporary, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
}
