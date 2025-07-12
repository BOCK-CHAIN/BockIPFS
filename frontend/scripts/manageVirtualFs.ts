import { createVirtualFs, addFile, addFolder, getByPath } from '../utils/virtualFs.ts';
import { saveVirtualFsToDisk } from '../utils/saveVirtualFs.ts';
import { loadVirtualFsFromDisk } from '../utils/loadVirtualFs.ts';

// Load or create base FS
let vfs = loadVirtualFsFromDisk();

// Add items if needed
vfs = addFolder(vfs, '/Documents/Projects');
vfs = addFile(vfs, '/Documents/Projects/demo.js', 'QmNewCID1');
vfs = addFile(vfs, '/Documents/readme.txt', 'QmNewCID2');

// Save changes
saveVirtualFsToDisk(vfs);

// Optional: print
console.log('Updated FS:', JSON.stringify(vfs, null, 2));
