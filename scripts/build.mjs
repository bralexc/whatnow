import { spawnSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
function run(command, args, cwd) {
	// npm is a .cmd wrapper on Windows; all shell arguments here are fixed literals.
	const result = spawnSync(command, args, {
		cwd, stdio: 'inherit', shell: command === 'npm' && process.platform === 'win32'
	});
	if (result.error) throw result.error;
	if (result.status !== 0) process.exit(result.status ?? 1);
}

run('npm', ['ci'], join(root, 'web'));
run('npm', ['run', 'build:embed'], join(root, 'web'));
mkdirSync(join(root, 'dist'), { recursive: true });
const windows = process.env.GOOS ? process.env.GOOS === 'windows' : process.platform === 'win32';
run('go', ['build', '-trimpath', '-o', join('dist', windows ? 'whatnow.exe' : 'whatnow'), './cmd/whatnow'], root);
