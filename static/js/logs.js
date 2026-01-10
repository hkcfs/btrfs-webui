import { API, openModal, closeModal } from './utils.js';

let logInterval;

export async function loadLogs() {
    const res = await fetch(`${API}/history`);
    const logs = await res.json();
    const render = (ls) => ls.map(l => `
        <div class="log-entry" onclick="this.classList.toggle('open')">
            <div class="log-header">
                <span>${l.emoji} ${l.type}</span>
                <span class="st-${l.status.split(' ')[0]}">${l.status}</span>
            </div>
            <div class="log-meta">${l.timestamp} • ${l.duration}</div>
            <div class="log-output">${l.output}</div>
        </div>
    `).join('');
    
    const dashLogs = document.getElementById('dashLogs');
    if(dashLogs) dashLogs.innerHTML = render(logs.slice(0, 8));
    
    const fullLogs = document.getElementById('fullLogs');
    if(fullLogs) fullLogs.innerHTML = render(logs);
}

export function pollLog(id) {
    openModal('outputModal');
    const el = document.getElementById('cmdOutput');
    el.innerText = "Waiting for output...";
    
    if(logInterval) clearInterval(logInterval);
    
    logInterval = setInterval(async () => {
        const res = await fetch(`${API}/history`);
        const logs = await res.json();
        const l = logs.find(x => x.id === id);
        if(l) {
            el.innerText = l.output || "Running...";
            if(l.status !== "Running...") clearInterval(logInterval);
        }
    }, 1000);
}

export async function clearLogs() {
    if(confirm("Clear log history?")) {
        await fetch(`${API}/logs/clear`);
        loadLogs();
    }
}
