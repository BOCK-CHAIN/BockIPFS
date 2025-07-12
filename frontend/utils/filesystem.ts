export type FileNode = {
  type: "file";
  name: string;
  cid: string;
  timestamp: string;
};

export type FolderNode = {
  type: "folder";
  name: string;
  children: { [name: string]: FileNode | FolderNode };
};

export const root: FolderNode = {
  type: "folder",
  name: "root",
  children: {},
};

export function addFolder(path: string): void {
  const parts = path.split("/").filter(Boolean);
  let current = root;

  for (const part of parts) {
    if (!current.children[part]) {
      current.children[part] = {
        type: "folder",
        name: part,
        children: {},
      };
    } else if (current.children[part].type !== "folder") {
      throw new Error(`"${part}" exists and is not a folder.`);
    }
    current = current.children[part] as FolderNode;
  }
}

export function addFile(path: string, filename: string, cid: string, timestamp = new Date().toISOString()): void {
  const parts = path.split("/").filter(Boolean);
  let current = root;

  for (const part of parts) {
    if (!current.children[part] || current.children[part].type !== "folder") {
      throw new Error(`Folder "${part}" does not exist.`);
    }
    current = current.children[part] as FolderNode;
  }

  current.children[filename] = {
    type: "file",
    name: filename,
    cid,
    timestamp,
  };
}

export function listFolderContents(path: string): (FileNode | FolderNode)[] {
  const parts = path.split("/").filter(Boolean);
  let current = root;

  for (const part of parts) {
    if (!current.children[part] || current.children[part].type !== "folder") {
      throw new Error(`Folder "${part}" does not exist.`);
    }
    current = current.children[part] as FolderNode;
  }

  return Object.values(current.children);
}
