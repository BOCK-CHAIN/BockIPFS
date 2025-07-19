// app/api/fs/add-file/route.ts
import { NextRequest, NextResponse } from 'next/server';

export const runtime = 'nodejs';

export async function POST(req: NextRequest) {
  console.log('🚀 API route /api/fs/add-file called');
  
  try {
    // 1. Extract file data from the incoming FormData
    console.log('📝 Step 1: Extracting form data...');
    const formData = await req.formData();
    const file = formData.get('file') as File | null;

    if (!file) {
      console.log('❌ No file provided in form data');
      return NextResponse.json({ error: 'No file provided' }, { status: 400 });
    }

    console.log(`✅ File received: ${file.name} (${file.size} bytes)`);

    // 2. Convert file to buffer
    console.log('📝 Step 2: Converting file to buffer...');
    const arrayBuffer = await file.arrayBuffer();
    const buffer = Buffer.from(arrayBuffer);
    console.log(`✅ Buffer created: ${buffer.length} bytes`);

    // 3. Use direct fetch to IPFS API instead of ipfs-http-client
    console.log('📝 Step 3: Uploading to IPFS via fetch...');
    
    try {
      // Create FormData for IPFS API
      const ipfsFormData = new FormData();
      // Create a new File object from the buffer
      const fileForIPFS = new File([buffer], file.name, { type: file.type });
      ipfsFormData.append('file', fileForIPFS);

      const ipfsResponse = await fetch('http://127.0.0.1:5001/api/v0/add', {
        method: 'POST',
        body: ipfsFormData,
      });

      if (!ipfsResponse.ok) {
        throw new Error(`IPFS API returned ${ipfsResponse.status}: ${ipfsResponse.statusText}`);
      }

      const ipfsResult = await ipfsResponse.text();
      console.log('IPFS raw response:', ipfsResult);

      // Parse the IPFS response to get the hash/CID
      const match = ipfsResult.match(/"Hash":"([^"]+)"/);
      if (!match) {
        throw new Error('Could not extract hash from IPFS response');
      }

      const cid = match[1];
      console.log(`✅ File uploaded to IPFS with CID: ${cid}`);

      // 4. Update VFS (with error handling)
      console.log('📝 Step 4: Updating Virtual File System...');
      try {
        const { loadVirtualFsFromDisk } = await import('@/utils/loadVirtualFs');
        const { addFile } = await import('@/utils/virtualFs');
        const { saveVirtualFsToDisk } = await import('@/utils/saveVirtualFs');

        const vfs = loadVirtualFsFromDisk();
        const filePath = `/${file.name}`;
        addFile(vfs, filePath, cid);
        saveVirtualFsToDisk(vfs);
        
        console.log(`✅ VFS updated with file: ${filePath}`);
      } catch (vfsError) {
        console.error('❌ VFS operations failed (but IPFS upload succeeded):', vfsError);
        // Return success anyway since IPFS upload worked
        return NextResponse.json({ 
          success: true, 
          cid: cid,
          path: `/${file.name}`,
          size: file.size,
          name: file.name,
          warning: 'File uploaded to IPFS but VFS update failed'
        });
      }

      // 5. Return success
      console.log('🎉 All operations completed successfully');
      return NextResponse.json({ 
        success: true, 
        cid: cid,
        path: `/${file.name}`,
        size: file.size,
        name: file.name
      });

    } catch (ipfsError) {
      console.error('❌ IPFS upload failed:', ipfsError);
      return NextResponse.json({ 
        error: 'IPFS upload failed',
        details: ipfsError instanceof Error ? ipfsError.message : 'Unknown IPFS error'
      }, { status: 500 });
    }

  } catch (error) {
    console.error('💥 Unexpected error in /api/fs/add-file:', error);
    console.error('Error stack:', error instanceof Error ? error.stack : 'No stack available');
    
    return NextResponse.json({ 
      error: 'Failed to upload file',
      details: error instanceof Error ? error.message : 'An unknown error occurred',
      type: error instanceof Error ? error.constructor.name : 'UnknownError'
    }, { status: 500 });
  }
}