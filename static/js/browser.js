import { API, openModal, closeModal } from './utils.js';

let currentPath = "";

export async function browse(path) {
    currentPath = path;
    openModal('browserModal');
    document.getElementById('browserPath').innerText = path;
    
    const res = await fetch(`${API}/browser/list?path=${encodeURIComponent(path)}`);
    if(!res.ok) { document.getElementById('fileList').innerHTML = "Access Denied"; return; }
    const files = await res.json();
    
    document.getElementById('fileList').innerHTML = files.map(f => `
        <div class="list-item" onclick="${f.is_dir ? `window.browse('${f.path.replace(/\\/g, "\\\\")}')` : `window.download('${f.path.replace(/\\/g, "\\\\")}')`}">
            <span style="display:flex; align-items:center; gap:10px;">
                ${f.is_dir ? '📁' : '📄'} ${f.name}
            </span>
            <span style="font-size:0.8rem; opacity:0.6">${(f.size/1024).toFixed(1)} KB</span>
        </div>
    `).join('');
}

export function browseUp() {
    const p = currentPath.split('/'); p.pop();
    browse(p.join('/'));
}

export function download(path) {
    window.location.href = `${API}/browser/download?path=${encodeURIComponent(path)}`;
}
