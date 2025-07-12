// app/fs/page.tsx
import { promises as fs } from 'fs';
import path from 'path';
import { VirtualFsTree } from '@/components/VirtualFsTree';
import type { VfsNode } from '@/components/VirtualFsTree';

export default async function FsPage() {
  const filePath = path.join(process.cwd(), 'data', 'virtualFs.json');
  const fileContent = await fs.readFile(filePath, 'utf-8');
  const vfs: VfsNode = JSON.parse(fileContent);

  return (
    <main className="p-4 bg-[#050816] text-white min-h-screen">
      <h1 className="text-2xl font-bold mb-4">📂 Virtual File System</h1>
      <VirtualFsTree node={vfs} />
    </main>
  );
}
