import { API } from './utils.js';
import { renderJobs } from './jobs.js';

export let appConfig = { jobs: [] };

export async function loadConfig() {
    try {
        console.log("[CONFIG-UI] Loading config from API...");
        const res = await fetch(`${API}/config`);
        console.log("[CONFIG-UI] Response status:", res.status, res.statusText);
        
        // FIX: Handle 401 without infinite reloading
        if(res.status === 401) {
            console.warn("[CONFIG-UI] Session expired or unauthorized.");
            document.body.innerHTML = `
                <div style="display:flex;justify-content:center;align-items:center;height:100vh;background:#050505;color:#fff;font-family:monospace;flex-direction:column;gap:20px;">
                    <h2>🔒 Session Expired</h2>
                    <button onclick="window.location.reload()" style="padding:10px 20px;background:#ff4d00;color:white;border:none;cursor:pointer;font-weight:bold;">Login Again</button>
                </div>`;
            return; 
        }
        
        if (!res.ok) {
            console.error("[CONFIG-UI] Error response:", res.status, res.statusText);
            alert("Error loading config: " + res.status);
            return;
        }
        
        appConfig = await res.json();
        console.log("[CONFIG-UI] Loaded config:", JSON.stringify(appConfig, null, 2));
        console.log("[CONFIG-UI] Jobs count:", appConfig.jobs ? appConfig.jobs.length : 0);
        
        if (appConfig.jobs && appConfig.jobs.length > 0) {
            console.log("[CONFIG-UI] First job:", JSON.stringify(appConfig.jobs[0], null, 2));
        }
        
        // Populate UI
        const driveInput = document.getElementById('target_drive');
        if(driveInput) {
            driveInput.value = appConfig.target_drive || '';
            console.log("[CONFIG-UI] Set target_drive to:", appConfig.target_drive || '');
        }
        
        const logLevel = document.getElementById('log_level');
        if(logLevel) logLevel.value = appConfig.log_level || 'DEFAULT';

        console.log("[CONFIG-UI] Rendering global scheds...");
        renderGlobalScheds();
        
        console.log("[CONFIG-UI] Rendering jobs...");
        renderJobs();
        
        // Show logout button if cookie exists
        if(document.cookie.includes('session_token')) {
            const btn = document.getElementById('logoutBtn');
            if(btn) btn.style.display = 'block';
        }
        
        console.log("[CONFIG-UI] Load complete!");
    } catch (e) {
        console.error("[CONFIG-UI] Config load failed", e);
        alert("Config load failed: " + e.message);
    }
}

export async function saveConfig() {
    console.log("[CONFIG-UI] saveConfig called");
    console.log("[CONFIG-UI] Current appConfig before save:", JSON.stringify(appConfig, null, 2));
    
    appConfig.target_drive = document.getElementById('target_drive').value;
    appConfig.log_level = document.getElementById('log_level').value;
    
    console.log("[CONFIG-UI] Sending to API:", JSON.stringify(appConfig, null, 2));
    
    try {
        const res = await fetch(`${API}/config`, { 
            method: 'POST', 
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(appConfig) 
        });
        
        console.log("[CONFIG-UI] Save response status:", res.status, res.statusText);
        
        if (!res.ok) {
            const errText = await res.text();
            console.error("[CONFIG-UI] Save error:", errText);
            alert("Save failed: " + errText);
            return;
        }
        
        const savedConfig = await res.json();
        console.log("[CONFIG-UI] Saved config response:", JSON.stringify(savedConfig, null, 2));
        
        alert("Saved successfully!");
        console.log("[CONFIG-UI] Reloading config...");
        await loadConfig();
        console.log("[CONFIG-UI] Reload complete, jobs count:", appConfig.jobs ? appConfig.jobs.length : 0);
    } catch (e) {
        console.error("[CONFIG-UI] Save failed:", e);
        alert("Save failed: " + e.message);
    }
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
