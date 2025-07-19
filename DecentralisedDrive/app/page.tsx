'use client';

import { useEffect, useState } from 'react';
import { VirtualFsTree } from '@/components/VirtualFsTree';
import type { VfsNode } from '@/utils/virtualFs';

export default function FsPage() {
  const [vfs, setVfs] = useState<VfsNode | null>(null);
  const [folderName, setFolderName] = useState('');
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [initialLoading, setInitialLoading] = useState(true);

  async function fetchVfs() {
    try {
      console.log('Fetching VFS from: /api/fs');
      const res = await fetch('/api/fs');
      console.log('Fetch response:', res.status, res.statusText);
      
      if (!res.ok) {
        throw new Error(`Failed to fetch VFS: ${res.status} ${res.statusText}`);
      }
      
      const data = await res.json();
      console.log('VFS data:', data);
      setVfs(data);
      setError(null);
    } catch (err) {
      console.error('Error fetching VFS:', err);
      setError(err instanceof Error ? err.message : 'Failed to fetch VFS');
    } finally {
      setInitialLoading(false);
    }
  }

  // Load VFS on component mount
  useEffect(() => {
    fetchVfs();
  }, []);

  async function handleAddFolder() {
    console.log('handleAddFolder called, folderName:', `"${folderName}"`);
    
    if (!folderName || !folderName.trim()) {
      console.log('Folder name validation failed');
      setError('Folder name cannot be empty');
      return;
    }

    setLoading(true);
    setError(null);
    
    try {
      const addFolderUrl = '/api/fs/add-folder';
      console.log('Adding folder to URL:', addFolderUrl);
      console.log('Request payload:', { path: `/${folderName.trim()}` });
      
      const res = await fetch(addFolderUrl, {
        method: 'POST',
        body: JSON.stringify({ path: `/${folderName.trim()}` }),
        headers: { 'Content-Type': 'application/json' },
      });

      console.log('Add folder response:', res.status, res.statusText);
      
      if (!res.ok) {
        const errorText = await res.text();
        console.log('Error response body:', errorText);
        throw new Error(`Failed to add folder: ${res.status} ${res.statusText} - ${errorText}`);
      }

      const result = await res.json();
      console.log('Add folder result:', result);
      
      setFolderName('');
      await fetchVfs();
    } catch (err) {
      console.error('Error adding folder:', err);
      setError(err instanceof Error ? err.message : 'Failed to add folder');
    } finally {
      setLoading(false);
    }
  }

  async function handleAddFile() {
    // 1. Check if a file is selected
    if (!selectedFile) {
      setError('Please select a file to upload.');
      return;
    }

    setLoading(true);
    setError(null);

    try {
      // 2. Create a FormData object to send the file
      const formData = new FormData();
      formData.append('file', selectedFile); 

      console.log('Uploading file:', selectedFile.name, `(${selectedFile.size} bytes)`);
      
      const res = await fetch('/api/fs/add-file', {
        method: 'POST',
        body: formData,
      });

      console.log('Upload response:', res.status, res.statusText);

      if (!res.ok) {
        const errorText = await res.text();
        console.log('Upload error response:', errorText);
        throw new Error(`Failed to upload file: ${res.status} ${res.statusText} - ${errorText}`);
      }

      const result = await res.json();
      console.log('Upload result:', result);

      // 4. Clear the selected file and file input
      setSelectedFile(null);
      // Reset the file input
      const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement;
      if (fileInput) fileInput.value = '';
      
      await fetchVfs();

    } catch (err) {
      console.error('Error uploading file:', err);
      setError(err instanceof Error ? err.message : 'Failed to upload file');
    } finally {
      setLoading(false);
    }
  }

  // Function to handle sharing files
  function handleShareFile(cid: string, fileName: string) {
    const ipfsUrl = `http://localhost:8080/ipfs/${cid}`;
    console.log(`Opening IPFS link for ${fileName}:`, ipfsUrl);
    window.open(ipfsUrl, '_blank');
  }

  if (initialLoading) {
    return (
      <main className="p-4 bg-[#050816] text-white min-h-screen">
        <div className="flex items-center justify-center h-64">
          <div className="text-lg">Loading Virtual File System...</div>
        </div>
      </main>
    );
  }

  return (
    <main className="p-4 bg-[#050816] text-white min-h-screen">
      <h1 className="text-2xl font-bold mb-4">📂 Virtual File System</h1>

      {error && (
        <div className="mb-4 p-3 bg-red-600 text-white rounded">
          <strong>Error:</strong> {error}
          <button 
            onClick={() => setError(null)}
            className="ml-2 text-xs underline hover:no-underline"
          >
            dismiss
          </button>
        </div>
      )}

      <div className="mb-6 space-y-4">
        <div className="flex items-center gap-2">
          <input
            value={folderName}
            onChange={(e) => {
              console.log('Folder input changed:', e.target.value);
              setFolderName(e.target.value);
              if (error) setError(null); // Clear error on input change
            }}
            placeholder="Enter folder name"
            className="bg-white text-black p-2 rounded border-2 border-white flex-1 max-w-xs"
            disabled={loading}
            onKeyPress={(e) => {
              if (e.key === 'Enter' && !loading) {
                handleAddFolder();
              }
            }}
          />
          <button 
            onClick={handleAddFolder} 
            className="bg-green-600 hover:bg-green-700 px-4 py-2 rounded disabled:opacity-50 disabled:cursor-not-allowed"
            disabled={loading || !folderName.trim()}
          >
            {loading ? 'Adding...' : 'Add Folder'}
          </button>
        </div>
        
        <div className="flex items-center gap-2">
          <input
            type="file"
            onChange={(e) => {
              const file = e.target.files?.[0] || null;
              console.log('File selected:', file?.name, file?.size, 'bytes');
              setSelectedFile(file);
              if (error) setError(null); // Clear error on file selection
            }}
            className="bg-white text-black p-2 rounded border-2 border-white flex-1"
            disabled={loading}
          />
          <button 
            onClick={handleAddFile} 
            className="bg-blue-600 hover:bg-blue-700 px-4 py-2 rounded disabled:opacity-50 disabled:cursor-not-allowed"
            disabled={loading || !selectedFile}
          >
            {loading ? 'Uploading...' : 'Upload to IPFS'}
          </button>
        </div>
        
        {selectedFile && (
          <div className="text-sm text-gray-300 bg-gray-800 p-2 rounded">
            <strong>Selected:</strong> {selectedFile.name} ({Math.round(selectedFile.size / 1024)} KB)
          </div>
        )}
      </div>

      <div className="border-t border-gray-700 pt-4">
        {vfs ? (
          <VirtualFsTree node={vfs} onShareFile={handleShareFile} />
        ) : (
          <div className="text-gray-400 text-center py-8">
            <div className="text-4xl mb-2">📁</div>
            <div>No virtual file system data available.</div>
            <div className="text-sm mt-1">Try adding a folder or uploading a file above.</div>
          </div>
        )}
      </div>
    </main>
  );
}