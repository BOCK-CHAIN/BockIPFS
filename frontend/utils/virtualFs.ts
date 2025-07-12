// utils/virtualFs.ts

export type VfsNode =
  | {
      name: string;
      type: 'file';
      cid: string;
      timestamp: string;
    }
  | {
      name: string;
      type: 'folder';
      children: VfsNode[];
    };

export function createVirtualFs(): VfsNode {
  return {
    name: '/',
    type: 'folder',
    children: []
  };
}

//input-path and output-vfsNode|null
function findNode(path: string, root: VfsNode): VfsNode | null {
  const parts = path.split('/').filter(Boolean);//filter- removes empty paths
  let current: VfsNode = root;
  for (const part of parts) {
    if (current.type !== 'folder') return null;
    const next = current.children.find(child => child.name === part);
    if (!next) return null;
    current = next;
  }
  return current;
}

export function addFolder(root: VfsNode, path: string): VfsNode {
  const parts = path.split('/').filter(Boolean);
  let current: VfsNode = root;
  for (const part of parts) {
    if (current.type !== 'folder') break;
    let next = current.children.find(child => child.name === part && child.type === 'folder') as VfsNode | undefined;
    if (!next) {
      next = { name: part, type: 'folder', children: [] };
      current.children.push(next);
    }
    current = next;
  }
  return root;
}

export function addFile(root: VfsNode, path: string, cid: string): VfsNode {
  const parts = path.split('/').filter(Boolean);
  const fileName = parts.pop();
  if (!fileName) return root;
  const folderPath = '/' + parts.join('/');
  const folder = findNode(folderPath, root);
  if (folder && folder.type === 'folder') {
    folder.children.push({ name: fileName, type: 'file', cid, timestamp: new Date().toISOString() });
  }
  return root;
}

export function getByPath(root: VfsNode, path: string): VfsNode | null {
  return findNode(path, root);
}
