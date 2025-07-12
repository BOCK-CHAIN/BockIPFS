import fs from 'fs';
import path from 'path';
import { VfsNode } from './virtualFs';

import { fileURLToPath } from 'url';


const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const filePath = path.join(__dirname, '../data/virtualFs.json');

export function saveVirtualFsToDisk(vfs: VfsNode) {
  fs.writeFileSync(filePath, JSON.stringify(vfs, null, 2), 'utf-8');
  console.log('✅ Virtual FS saved to disk.');
}
