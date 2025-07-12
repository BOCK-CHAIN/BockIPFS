// components/VirtualFsTree.tsx

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
}

export function VirtualFsTree({ node, depth = 0 }: Props) {
  const indent = 'pl-' + depth * 4;

  if (node.type === 'folder') {
    return (
      <div className={`${indent} text-yellow-300`}>
        📁 {node.name}
        <div className="ml-4">
          {node.children.map((child, i) => (
            <VirtualFsTree key={i} node={child} depth={depth + 1} />
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className={`${indent} text-green-300`}>
      📄 {node.name} <span className="text-gray-400">({node.cid})</span>
    </div>
  );
}
