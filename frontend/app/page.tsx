"use client";

import { useRef, useState } from "react";
import { CloudUpload } from "lucide-react";

export default function UploadButton() {
  const inputRef = useRef<HTMLInputElement | null>(null);
  const [uploadedHash, setUploadedHash] = useState("");
  const [inputHash, setInputHash] = useState("");
  const [displayHash, setDisplayHash] = useState("");

  const handleUploadClick = () => inputRef.current?.click();

  const handleFileChange = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const files = event.target.files;
    if (!files || files.length === 0) return;

    const file = files[0];
    const formData = new FormData();
    formData.append("file", file);

    const res = await fetch("http://127.0.0.1:5001/api/v0/add", {
      method: "POST",
      body: formData,
    });

    const text = await res.text();
    const match = text.match(/"Hash":"(.*?)"/);
    if (match) setUploadedHash(match[1]);
  };

  return (
    <div className="mx-8 my-6 text-white space-y-6">
      <button
        type="button"
        onClick={handleUploadClick}
        className="flex items-center gap-2 px-8 py-5 bg-amber-50 hover:bg-gray-300 text-black font-semibold rounded-2xl shadow-md text-xl"
      >
        <CloudUpload className="w-5 h-5" />
        Upload files
      </button>

      

      {uploadedHash && (
        <p className="text-white">Uploaded Hash: {uploadedHash}</p>
      )}

      <div className="space-x-2">
        <input
          type="text"
          placeholder="Enter CID"
          value={inputHash}
          onChange={(e) => setInputHash(e.target.value)}
          className="px-4 py-2 rounded-lg text-black bg-amber-50"
        />
        <button
          onClick={() => setDisplayHash(inputHash)}
          className="bg-blue-600 hover:bg-blue-800 px-4 py-2 rounded-xl"
        >
          Retrieve
        </button>
      </div>

      {displayHash && (
        <img
          src={`http://127.0.0.1:8080/ipfs/${displayHash}`}
          alt="From IPFS"
          className="max-w-md mt-4 rounded-lg"
        />
      )}
    </div>
  );
}
