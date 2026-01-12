import { API, openModal, closeModal } from './utils.js';
import { appConfig, saveConfig } from './config.js';
import { loadLogs, pollLog } from './logs.js';
import { browse, download } from './browser.js';

let currentJobId = "";
let selectedSnaps = [];

export function renderJobs() {
    const list = document.getElementById('jobList');
    if(!list) return;
    if(!appConfig.jobs || appConfig.jobs.length === 0) { 
        list.innerHTML = "<p style='padding:20px; text-align:center; color:gray'>No jobs configured.</p>"; return; 
    }
    
    list.innerHTML = appConfig.jobs.map((j, i) => {
        const sched = j.schedule.enabled ? `${j.schedule.value} ${j.schedule.unit}` : 'Disabled';
        const ret = j.retention.enabled 
            ? `Keep ${j.retention.value} ${j.retention.mode === 'time' ? j.retention.unit : 'Snaps'}` 
            : 'Unlimited';
        
        setTimeout(() => loadJobStats(j.id), 200);

        return `
        <div class="job-card">
            <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:12px; border-bottom:1px solid var(--border); padding-bottom:8px;">
                <h3 style="margin:0; font-size:1.1rem">📁 ${j.name}</h3>
                <div class="btn-group">
                    <button class="btn btn-sm" onclick="window.editJob(${i})">Edit</button>
                    <button class="btn btn-primary btn-sm" onclick="window.runJob('${j.id}')">Run</button>
                </div>
            </div>
            
            <div style="display:grid; grid-template-columns:1fr 1fr; gap:8px; font-size:0.85rem; margin-bottom:12px; color:var(--text); opacity:0.9;">
                <div><span style="opacity:0.6">Source:</span> <span title="${j.source}">${shortPath(j.source)}</span></div>
                <div><span style="opacity:0.6">Dest:</span> <span title="${j.dest}">${shortPath(j.dest)}</span></div>
                <div><span style="opacity:0.6">Sched:</span> ${sched}</div>
                <div><span style="opacity:0.6">Retain:</span> ${ret}</div>
            </div>

            <!-- Stats Container -->
            <div id="stats-${j.id}" style="background:var(--bg); border:1px solid var(--border); padding:8px 12px; border-radius:6px; font-size:0.85rem; margin-bottom:12px; display:flex; justify-content:space-between; align-items:center;">
                <span style="opacity:0.6">Loading Stats...</span>
            </div>

            <button class="btn btn-sec btn-sm" style="width:100%; padding:8px;" onclick="window.viewSnapshots('${j.id}')">📂 Manage Snapshots</button>
        </div>
    `}).join('');
}

function shortPath(path) {
    if(!path) return "";
    return path.length > 25 ? '...'+path.slice(-25) : path;
}

async function loadJobStats(jobId) {
    try {
        const res = await fetch(`${API}/snapshots/stats?job_id=${jobId}`);
        if(res.ok) {
            const stats = await res.json();
            const el = document.getElementById(`stats-${jobId}`);
            if(el) {
                el.innerHTML = `
                    <div>📸 Total: <b>${stats.count}</b></div>
                    <div>👴 Oldest: <b>${stats.oldest}</b></div>
                `;
            }
        }
    } catch(e) {}
}

export function editJob(idx) {
    openModal('jobModal');
    if(idx === 'new') {
        document.getElementById('jobForm').reset();
        document.getElementById('j_id').value = "new";
        toggleJobRetentionUI();
    } else {
        const j = appConfig.jobs[idx];
        document.getElementById('j_id').value = j.id;
        document.getElementById('j_name').value = j.name;
        document.getElementById('j_src').value = j.source;
        document.getElementById('j_dest').value = j.dest;
        
        document.getElementById('j_sched_en').checked = j.schedule.enabled;
        document.getElementById('j_sched_type').value = j.schedule.type;
        document.getElementById('j_sched_val').value = j.schedule.value;
        document.getElementById('j_sched_unit').value = j.schedule.unit;
        
        document.getElementById('j_ret_en').checked = j.retention.enabled;
        document.getElementById('j_ret_mode').value = j.retention.mode;
        document.getElementById('j_ret_val').value = j.retention.value;
        document.getElementById('j_ret_unit').value = j.retention.unit;
        
        document.getElementById('j_pre').value = j.pre_command || "";
        document.getElementById('j_post').value = j.post_command || "";
        document.getElementById('j_repl_en').checked = j.replication?.enabled || false;
        document.getElementById('j_repl_dest').value = j.replication?.target_dest || "";
        
        toggleJobRetentionUI();
    }
}

export function saveJobForm(e) {
    e.preventDefault();
    const id = document.getElementById('j_id').value;
    const job = {
        id: id === "new" ? Date.now().toString() : id,
        name: document.getElementById('j_name').value,
        source: document.getElementById('j_src').value,
        dest: document.getElementById('j_dest').value,
        schedule: {
            enabled: document.getElementById('j_sched_en').checked,
            type: document.getElementById('j_sched_type').value,
            value: document.getElementById('j_sched_val').value,
            unit: document.getElementById('j_sched_unit').value,
        },
        retention: {
            enabled: document.getElementById('j_ret_en').checked,
            mode: document.getElementById('j_ret_mode').value,
            value: parseInt(document.getElementById('j_ret_val').value),
            unit: document.getElementById('j_ret_unit').value,
        },
        pre_command: document.getElementById('j_pre').value,
        post_command: document.getElementById('j_post').value,
        replication: {
            enabled: document.getElementById('j_repl_en').checked,
            target_dest: document.getElementById('j_repl_dest').value
        }
    };

    if(id === "new") { 
        if(!appConfig.jobs) appConfig.jobs = [];
        appConfig.jobs.push(job); 
    } else { 
        const i = appConfig.jobs.findIndex(x=>x.id===id); 
        if(i>-1) appConfig.jobs[i]=job; 
    }
    
    saveConfig();
    closeModal('jobModal');
}

export function deleteJob() {
    if(!confirm("Delete Job?")) return;
    const id = document.getElementById('j_id').value;
    appConfig.jobs = appConfig.jobs.filter(x => x.id !== id);
    saveConfig();
    closeModal('jobModal');
}

export function toggleJobRetentionUI() {
    const isTime = document.getElementById('j_ret_mode').value === 'time';
    const el = document.getElementById('j_ret_unit');
    if(isTime) {
        el.classList.remove('disabled-input');
        el.disabled = false;
        el.style.opacity = "1";
    } else {
        el.classList.add('disabled-input');
        el.disabled = true;
        el.style.opacity = "0.3";
    }
}

export async function runJob(id) {
    if(!confirm("Run job?")) return;
    await fetch(`${API}/action/job?id=${id}`);
    loadLogs();
}

export async function viewSnapshots(jobId) {
    currentJobId = jobId;
    selectedSnaps = [];
    openModal('snapListModal');
    
    document.getElementById('snapListContainer').innerHTML = "<div style='padding:20px;text-align:center'>Loading...</div>";

    const res = await fetch(`${API}/snapshots/list?job_id=${jobId}`);
    const list = await res.json();
    
    // New Clean Layout
    document.getElementById('snapListContainer').innerHTML = list.length ? list.map(s => `
        <div style="
            display: grid; 
            grid-template-columns: 20px 1fr auto; 
            gap: 15px; 
            align-items: center; 
            padding: 12px; 
            border-bottom: 1px solid var(--border);
            transition: background 0.2s;
        " onmouseover="this.style.background='var(--bg)'" onmouseout="this.style.background='transparent'">
            
            <input type="checkbox" style="width:18px; height:18px; cursor:pointer;" onchange="window.selectSnap(this, '${s.path}')">
            
            <div style="overflow: hidden;">
                <div style="font-weight: 600; font-size: 0.95rem; color: var(--text);">${s.date}</div>
                <div style="font-family: monospace; font-size: 0.8rem; opacity: 0.5; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">
                    ${s.name}
                </div>
            </div>
            
            <div style="display: flex; gap: 8px;">
                <button class="btn btn-sm" onclick="window.rollback('${s.path}')" title="Restore this version">♻️</button>
                <button class="btn btn-sm" onclick="window.browse('${s.path}')" title="Browse Files">📂</button>
                <button class="btn btn-danger btn-sm" onclick="window.delSnap('${s.path}')" title="Delete">🗑</button>
            </div>
        </div>
    `).join('') : "<div style='padding:20px;text-align:center'>No snapshots found.</div>";
}

export function selectSnap(cb, path) {
    if(cb.checked) selectedSnaps.push(path);
    else selectedSnaps = selectedSnaps.filter(p => p !== path);
}

export async function compareSnapshots() {
    if(selectedSnaps.length !== 2) { alert("Select exactly 2 snapshots."); return; }
    const res = await fetch(`${API}/snapshots/diff?a=${encodeURIComponent(selectedSnaps[0])}&b=${encodeURIComponent(selectedSnaps[1])}`);
    const data = await res.json();
    pollLog(data.id);
}

export async function delSnap(path) {
    if(confirm("Delete?")) {
        await fetch(`${API}/snapshots/delete?path=${encodeURIComponent(path)}`);
        viewSnapshots(currentJobId);
        loadJobStats(currentJobId);
    }
}

export async function rollback(path) {
    if(confirm("⚠ WARNING: This will overwrite your current live data with this snapshot!\n\nThe current state will be saved as a backup folder before restoring.\n\nContinue?")) {
        const res = await fetch(`${API}/snapshots/rollback?job_id=${currentJobId}&path=${encodeURIComponent(path)}`);
        if(res.ok) {
            alert("Rollback triggered! Check logs for status.");
            loadLogs();
        }
    }
}

export async function purgeAll() {
    if(prompt("Type DELETE to confirm wiping all snapshots for ALL jobs:") === "DELETE") {
        await fetch(`${API}/action/purge_all`);
        loadLogs();
    }
}
