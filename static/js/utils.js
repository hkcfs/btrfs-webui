export const API = '/api';

export function toGB(b) {
    if (!b) return "0 GB";
    return (b / 1024 / 1024 / 1024).toFixed(1) + " GB";
}

export function openModal(id) { document.getElementById(id).classList.add('active'); }
export function closeModal(id) { document.getElementById(id).classList.remove('active'); }

export function toggleTheme() {
    const b = document.body;
    const current = b.getAttribute('data-theme');
    const next = current === 'light' ? '' : 'light';
    if (next) b.setAttribute('data-theme', 'light');
    else b.removeAttribute('data-theme');
    localStorage.setItem('theme', next);
}

if(localStorage.getItem('theme') === 'light') document.body.setAttribute('data-theme', 'light');

export function logout() { 
    // Set expiry to past to delete
    document.cookie = "session_token=; expires=Thu, 01 Jan 1970 00:00:00 UTC; path=/;"; 
    // Redirect home (which will be intercepted by auth middleware if password set)
    window.location.href = '/'; 
}
