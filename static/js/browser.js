import { API, openModal, closeModal } from './utils.js';

let currentPath = "";

export async function browse(path) {
    currentPath = path;
    openModal('browserModal');
    
    // Breadcrumb style path
    const parts = path.split('/').filter(p => p);
    let breadcrumbHtml = `<span onclick="window.browse('/')" style="cursor:pointer;color:var(--accent)">/</span>`;
    let buildPath = "";
    parts.forEach((p, i) => {
        buildPath += "/" + p;
        const safePath = buildPath.replace(/\\/g, "\\\\"); // Escape backslashes for JS string
        if (i === parts.length - 1) {
            breadcrumbHtml += ` <span style="opacity:0.5">/</span> <b>${p}</b>`;
        } else {
            breadcrumbHtml += ` <span style="opacity:0.5">/</span> <span onclick="window.browse('${safePath}')" style="cursor:pointer;color:var(--accent)">${p}</span>`;
        }
    });
    
    document.getElementById('browserPath').innerHTML = breadcrumbHtml;
    document.getElementById('fileList').innerHTML = "<div style='padding:20px;text-align:center'>Loading...</div>";
    
    const res = await fetch(`${API}/browser/list?path=${encodeURIComponent(path)}`);
    if(!res.ok) { document.getElementById('fileList').innerHTML = "<div style='padding:20px;text-align:center;color:red'>Access Denied</div>"; return; }
    const files = await res.json();
    
    if(files.length === 0) {
        document.getElementById('fileList').innerHTML = "<div style='padding:20px;text-align:center;opacity:0.6'>Empty Folder</div>";
        return;
    }

    document.getElementById('fileList').innerHTML = files.map(f => {
        const icon = f.is_dir ? '📁' : '📄';
        const action = f.is_dir ? `window.browse('${f.path.replace(/\\/g, "\\\\")}')` : `window.download('${f.path.replace(/\\/g, "\\\\")}')`;
        const size = f.is_dir ? '' : formatSize(f.size);
        
        return `
        <div class="list-item" onclick="${action}" style="cursor:pointer; padding:8px 12px;">
            <div style="display:flex; align-items:center; gap:12px;">
                <span style="font-size:1.2rem">${icon}</span>
                <span style="font-weight:500">${f.name}</span>
            </div>
            <span style="font-size:0.85rem; opacity:0.6; font-family:monospace">${size}</span>
        </div>
    `}).join('');
}

function formatSize(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

export function browseUp() {
    const p = currentPath.split('/'); 
    p.pop(); 
    // If root
    if(p.length === 0 || (p.length === 1 && p[0] === "")) browse("/");
    else browse(p.join('/'));
}

export function download(path) {
    window.location.href = `${API}/browser/download?path=${encodeURIComponent(path)}`;
}
