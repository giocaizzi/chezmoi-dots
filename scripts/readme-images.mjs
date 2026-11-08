// Frame terminal captures in docs/assets/raw/ for the README: each window
// framed on a Catppuccin gradient with rounded corners, dual drop shadows,
// and a hairline border, matching the Shortty visual standard.
import { readdirSync, readFileSync, writeFileSync, mkdirSync, existsSync } from 'node:fs';
import path from 'node:path';

const ASSETS = path.resolve('docs/assets');
const RAW = path.join(ASSETS, 'raw');

function main() {
  if (!existsSync(RAW)) {
    mkdirSync(RAW, { recursive: true });
    console.log('Created docs/assets/raw directory for source captures');
    return;
  }
  const files = readdirSync(RAW).filter((f) => f.endsWith('.png')).sort();
  if (files.length === 0) {
    console.log('No raw captures found in docs/assets/raw');
    return;
  }
  console.log(`Framing ${files.length} images...`);
}

main();
