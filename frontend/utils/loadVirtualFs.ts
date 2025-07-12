import fs from 'fs';
import path from 'path';
import { VfsNode } from './virtualFs';


import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const filePath = path.join(__dirname, '../data/virtualFs.json');

export function loadVirtualFsFromDisk(): VfsNode {
  if (!fs.existsSync(filePath)) {
    return { name: '/', type: 'folder', children: [] };
  }
  const raw = fs.readFileSync(filePath, 'utf-8');
  return JSON.parse(raw);
}
