import { API } from './utils.js';
import { renderJobs } from './jobs.js';

export let appConfig = { jobs: [] };

export async function loadConfig() {
    const res = await fetch(`${API}/config`);
    if(res.status === 401) { 
        document.body.innerHTML = "<h2 style='text-align:center;margin-top:50px'>Unauthorized. <a href='/api/login'>Login</a></h2>"; 
        return; 
    }
    appConfig = await res.json();
    
    // Global Settings UI
    const driveInput = document.getElementById('target_drive');
    if(driveInput) driveInput.value = appConfig.target_drive || '';
    
    const logLevel = document.getElementById('log_level');
    if(logLevel) logLevel.value = appConfig.log_level || 'DEFAULT';

    // Global Schedules
    renderGlobalScheds();
    renderJobs(); // Trigger job re-render
    
    if(document.cookie.includes('session_token')) {
        document.getElementById('logoutBtn').style.display = 'block';
    }
}

export async function saveConfig() {
    appConfig.target_drive = document.getElementById('target_drive').value;
    appConfig.log_level = document.getElementById('log_level').value;
    
    await fetch(`${API}/config`, { method: 'POST', body: JSON.stringify(appConfig) });
    alert("Saved");
    loadConfig();
}

function renderGlobalScheds() {
    const container = document.getElementById('globalScheds');
    if(!container) return;
    
    const renderRow = (key, name, c) => `
        <div style="display:flex; justify-content:space-between; align-items:center; background:var(--bg); padding:10px; border-radius:6px; margin-bottom:5px;">
            <span>${name}</span>
            <div style="display:flex; gap:5px; align-items:center;">
                <input type="checkbox" style="width:auto" ${c.enabled?'checked':''} onchange="window.updateSched('${key}', 'enabled', this.checked)">
                <input type="text" value="${c.value}" style="width:60px" onchange="window.updateSched('${key}', 'value', this.value)">
                <select style="width:80px" onchange="window.updateSched('${key}', 'unit', this.value)">
                    <option value="days" ${c.unit=='days'?'selected':''}>Days</option>
                    <option value="hours" ${c.unit=='hours'?'selected':''}>Hrs</option>
                </select>
            </div>
        </div>`;
    
    container.innerHTML = 
        renderRow('scrub_sched', 'Auto Scrub', appConfig.scrub_sched) +
        renderRow('balance_sched', 'Auto Balance', appConfig.balance_sched);
}

// Global hook for inline onchange events
window.updateSched = (key, field, val) => {
    appConfig[key][field] = val;
};
