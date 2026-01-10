import { API, toGB } from './utils.js';
import { loadLogs, pollLog } from './logs.js';

export async function loadDashboard() {
    loadLogs();
    
    // Storage
    try {
        const sRes = await fetch(`${API}/storage/usage`);
        const sData = await sRes.json();
        document.getElementById('storageLoading').style.display = 'none';
        document.getElementById('storageContent').style.display = 'block';
        
        document.getElementById('valUsed').innerText = toGB(sData.used);
        document.getElementById('valMeta').innerText = toGB(sData.metadata_used);
        document.getElementById('valFree').innerText = toGB(sData.free);
        document.getElementById('valUnalloc').innerText = toGB(sData.device_unallocated);

        const pUsed = (sData.used / sData.device_size) * 100;
        const pMeta = (sData.metadata_used / sData.device_size) * 100;
        const pFree = 100 - pUsed - pMeta;

        document.getElementById('barUsed').style.width = pUsed + "%";
        document.getElementById('barMeta').style.width = pMeta + "%";
        document.getElementById('barFree').style.width = pFree + "%";
    } catch(e) { console.error(e); }

    // BTRFS Stats
    try {
        const bRes = await fetch(`${API}/health/btrfs`);
        const bData = await bRes.json();
        let html = "";
        for(const [dev, stats] of Object.entries(bData)) {
            let rows = "";
            for(const [k,v] of Object.entries(stats)) {
                let cls = (v === "OK") ? "ok-bold" : (v != "0" ? "err-red" : "");
                rows += `<div>${k}: <span class="${cls}">${v}</span></div>`;
            }
            html += `<div style="margin-bottom:5px;"><strong>${dev}</strong>${rows}</div>`;
        }
        document.getElementById('btrfsStats').innerHTML = html || "No stats";
    } catch(e) {}

    // SMART
    try {
        const smRes = await fetch(`${API}/health/smart`);
        const smData = await smRes.json();
        const s = smData.smart_status?.passed ? "PASSED" : "FAILED";
        const c = smData.smart_status?.passed ? "#10b981" : "#ef4444";
        document.getElementById('smartData').innerHTML = `
            <div>Model: <b>${smData.model_name || 'Unknown'}</b></div>
            <div>Serial: <b>${smData.serial_number || 'N/A'}</b></div>
            <div>Status: <b style="color:${c}">${s}</b></div>
            <div>Temp: <b>${smData.temperature?.current || '?'}°C</b></div>
            <div>Power On: <b>${smData.power_on_time?.hours || '?'} Hrs</b></div>
        `;
    } catch(e) {}
}

export async function doAction(type, action, output=false) {
    if(!confirm("Are you sure?")) return;
    const res = await fetch(`${API}/action/${type}?action=${action}`);
    const data = await res.json();
    if(output) pollLog(data.id); else { alert("Started"); loadLogs(); }
}

export function runSmart(type) {
    fetch(`${API}/health/test?type=${type}`).then(r=>r.json()).then(d=>pollLog(d.id));
}
