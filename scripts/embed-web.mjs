import { cpSync, existsSync, readdirSync, rmSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { join } from 'node:path';

const source = fileURLToPath(new URL('../web/build/', import.meta.url));
const destination = fileURLToPath(new URL('../internal/web/dist/', import.meta.url));
if (!existsSync(join(source, '200.html'))) {
	throw new Error('Build the web frontend before embedding it (missing web/build/200.html).');
}
// Preserve the tracked placeholder and replace only generated assets.
for (const name of readdirSync(destination)) {
	if (name !== '.gitkeep') rmSync(join(destination, name), { recursive: true, force: true });
}
cpSync(source, destination, { recursive: true });
