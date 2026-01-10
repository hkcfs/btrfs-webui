export const API = '/api';

export function toGB(b) {
    return (b / 1024 / 1024 / 1024).toFixed(1) + " GB";
}

export function openModal(id) {
    document.getElementById(id).classList.add('active');
}

export function closeModal(id) {
    document.getElementById(id).classList.remove('active');
}

export function toggleTheme() {
    const b = document.body;
    const current = b.getAttribute('data-theme');
    const next = current === 'dark' ? 'light' : 'dark';
    b.setAttribute('data-theme', next);
    localStorage.setItem('theme', next);
}

// Initial Theme Load
if(localStorage.getItem('theme') === 'dark') document.body.setAttribute('data-theme', 'dark');
