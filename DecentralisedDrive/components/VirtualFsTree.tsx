'use client';

import React from 'react';

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

interface Props {
  node: VfsNode;
  depth?: number;
  onShareFile?: (cid: string, fileName: string) => void;
}

export function VirtualFsTree({ node, depth = 0, onShareFile }: Props) {
  const indent = 'pl-' + depth * 4;

  if (node.type === 'folder') {
    return (
      <div className={`${indent} text-yellow-300`}>
        📁 {node.name}
        <div className="ml-4">
          {node.children.map((child, i) => (
            <VirtualFsTree key={i} node={child} depth={depth + 1} onShareFile={onShareFile} />
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className={`${indent} text-green-300 flex items-center justify-between group hover:bg-gray-800 py-1 px-2 rounded`}>
      <div>
        📄 {node.name} <span className="text-gray-400">({node.cid})</span>
      </div>
      
      {onShareFile && (
        <button
          onClick={() => {
            console.log('Share button clicked for:', node.name, node.cid);
            onShareFile(node.cid, node.name);
          }}
          className="bg-white hover:bg-gray-200 text-black px-2 py-1 rounded text-xs
                     flex items-center gap-1 ml-2 border border-gray-400"
          title={`Share via IPFS: ${node.cid}`}
        >
          <span>🔗</span>
          <span>Share</span>
        </button>
      )}
    </div>
  );
}