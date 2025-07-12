export type FileNode = {
  type: "file";
  name: string;
  cid: string; // IPFS hash
  createdAt: string;
};

export type FolderNode = {
  type: "folder";
  name: string;
  children: (FileNode | FolderNode)[];
};

export type FileSystemRoot = FolderNode;

// Example root directory
export const root: FileSystemRoot = {
  type: "folder",
  name: "root",
  children: []
};