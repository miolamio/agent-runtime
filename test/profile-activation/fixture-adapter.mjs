// Only catalog transport/installation is a fixture. Production preparation,
// inventory verification, cache publication, native installation and activation run.
import { prepare } from '/airun/component-adapter.mjs';
import { fixtureDependencies } from './fixtures.mjs';
const args = process.argv.slice(2);
const options = Object.fromEntries(Array.from({ length: args.length / 2 }, (_, index) => [args[index * 2].slice(2), args[index * 2 + 1]]));
try { await prepare(options, fixtureDependencies()); }
catch (error) { console.error(error.message); process.exitCode = 1; }
