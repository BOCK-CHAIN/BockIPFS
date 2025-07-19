// app/api/fs/add-folder/route.ts
import { NextRequest, NextResponse } from 'next/server';
import { loadVirtualFsFromDisk } from '@/utils/loadVirtualFs';
import { addFolder } from '@/utils/virtualFs';



export async function POST(req: NextRequest) {
    console.log('API HIT')
  try {
    const { path } = await req.json(); // ✅ read once
    const vfs = loadVirtualFsFromDisk();
    addFolder(vfs, path);

    return NextResponse.json({ success: true });
  } catch (err) {
    console.error('Error in add-folder:', err);
    return NextResponse.json({ error: 'Failed to add folder' }, { status: 500 });
  }
}

