import { API } from './utils.js';
import { renderJobs } from './jobs.js';

export let appConfig = { jobs: [] };

export async function loadConfig() {
    try {
        const res = await fetch(`${API}/config`);
        
        // FIX: Handle 401 without infinite reloading
        if(res.status === 401) {
            console.warn("Session expired or unauthorized.");
            document.body.innerHTML = `
                <div style="display:flex;justify-content:center;align-items:center;height:100vh;background:#050505;color:#fff;font-family:monospace;flex-direction:column;gap:20px;">
                    <h2>🔒 Session Expired</h2>
                    <button onclick="window.location.reload()" style="padding:10px 20px;background:#ff4d00;color:white;border:none;cursor:pointer;font-weight:bold;">Login Again</button>
                </div>`;
            return; 
        }
        
        appConfig = await res.json();
        
        // Populate UI
        const driveInput = document.getElementById('target_drive');
        if(driveInput) driveInput.value = appConfig.target_drive || '';
        
        const logLevel = document.getElementById('log_level');
        if(logLevel) logLevel.value = appConfig.log_level || 'DEFAULT';

        renderGlobalScheds();
        renderJobs();
        
        // Show logout button if cookie exists
        if(document.cookie.includes('session_token')) {
            const btn = document.getElementById('logoutBtn');
            if(btn) btn.style.display = 'block';
        }
    } catch (e) {
        console.error("Config load failed", e);
    }
}

export async function saveConfig() {
    appConfig.target_drive = document.getElementById('target_drive').value;
    appConfig.log_level = document.getElementById('log_level').value;
    await fetch(`${API}/config`, { method: 'POST', body: JSON.stringify(appConfig) });
    alert("Saved");
    loadConfig();
}

export async function saveGlobalConfig(e) {
    e.preventDefault();
    await saveConfig();
}

export function exportConfig() {
    const dataStr = "data:text/json;charset=utf-8," + encodeURIComponent(JSON.stringify(appConfig, null, 2));
    const downloadAnchorNode = document.createElement('a');
    downloadAnchorNode.setAttribute("href", dataStr);
    downloadAnchorNode.setAttribute("download", "btrfs_commander_config.json");
    document.body.appendChild(downloadAnchorNode);
    downloadAnchorNode.click();
    downloadAnchorNode.remove();
}

export function importConfig(input) {
    const file = input.files[0];
    if (!file) return;

    const reader = new FileReader();
    reader.onload = async (e) => {
        try {
            const imported = JSON.parse(e.target.result);
            if (!imported.jobs) throw new Error("Invalid config format");
            
            if (confirm("This will overwrite current jobs and settings. Proceed?")) {
                appConfig = imported;
                await fetch(`${API}/config`, { method: 'POST', body: JSON.stringify(appConfig) });
                alert("Configuration Restored Successfully");
                window.location.reload();
            }
        } catch (err) {
            alert("Failed to import: " + err.message);
        }
    };
    reader.readAsText(file);
}

function renderGlobalScheds() {
    const container = document.getElementById('globalScheds');
    if(!container) return;
    
    const renderRow = (key, name, c) => `
        <div style="display:flex; justify-content:space-between; align-items:center; background:var(--bg-app); padding:10px; border-radius:6px; margin-bottom:5px; border:1px solid var(--border);">
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

window.updateSched = (key, field, val) => {
    appConfig[key][field] = val;
};
