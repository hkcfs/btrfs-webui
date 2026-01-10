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
        list.innerHTML = "<p style='padding:20px; text-align:center'>No jobs configured.</p>"; return; 
    }
    
    list.innerHTML = appConfig.jobs.map((j, i) => `
        <div class="job-card">
            <div style="display:flex; justify-content:space-between; margin-bottom:10px;">
                <h3 style="margin:0">${j.name}</h3>
                <div>
                    <button class="btn btn-sm" onclick="window.editJob(${i})">Edit</button>
                    <button class="btn btn-primary btn-sm" onclick="window.runJob('${j.id}')">Run Now</button>
                </div>
            </div>
            <div style="display:grid; grid-template-columns:1fr 1fr; gap:10px; font-size:0.9rem; opacity:0.8;">
                <div>Src: ${j.source}</div>
                <div>Dest: ${j.dest}</div>
                <div>Sched: ${j.schedule.enabled ? j.schedule.value + ' ' + j.schedule.unit : 'Off'}</div>
                <div>Hooks: ${j.pre_command || j.post_command ? '✅' : '❌'}</div>
            </div>
            <button class="btn btn-sm" style="width:100%; margin-top:10px;" onclick="window.viewSnapshots('${j.id}')">📂 View Snapshots</button>
        </div>
    `).join('');
}

export function editJob(idx) {
    openModal('jobModal');
    if(idx === 'new') {
        document.getElementById('jobForm').reset();
        document.getElementById('j_id').value = "new";
    } else {
        const j = appConfig.jobs[idx];
        document.getElementById('j_id').value = j.id;
        document.getElementById('j_name').value = j.name;
        document.getElementById('j_src').value = j.source;
        document.getElementById('j_dest').value = j.dest;
        
        // Schedule
        document.getElementById('j_sched_en').checked = j.schedule.enabled;
        document.getElementById('j_sched_type').value = j.schedule.type;
        document.getElementById('j_sched_val').value = j.schedule.value;
        document.getElementById('j_sched_unit').value = j.schedule.unit;
        
        // Retention
        document.getElementById('j_ret_en').checked = j.retention.enabled;
        document.getElementById('j_ret_mode').value = j.retention.mode;
        document.getElementById('j_ret_val').value = j.retention.value;
        document.getElementById('j_ret_unit').value = j.retention.unit;
        
        // Advanced Hooks & Replication (RESTORED HERE)
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
        // Capture Advanced Fields
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
    } else {
        el.classList.add('disabled-input');
        el.disabled = true;
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
    
    const res = await fetch(`${API}/snapshots/list?job_id=${jobId}`);
    const list = await res.json();
    
    document.getElementById('snapListContainer').innerHTML = list.length ? list.map(s => `
        <div class="list-item">
            <div style="display:flex; align-items:center; gap:10px;">
                <input type="checkbox" onchange="window.selectSnap(this, '${s.path}')">
                <div>
                    <div style="font-weight:bold; font-family:monospace">${s.name}</div>
                    <div style="font-size:0.8rem; opacity:0.6">${s.date}</div>
                </div>
            </div>
            <div style="display:flex; gap:5px;">
                <button class="btn btn-sm" onclick="window.rollback('${s.path}')" title="Restore">♻️</button>
                <button class="btn btn-sm" onclick="window.browse('${s.path}')">📂</button>
                <button class="btn btn-danger btn-sm" onclick="window.delSnap('${s.path}')">🗑</button>
            </div>
        </div>
    `).join('') : "No snapshots found.";
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
    }
}

export async function rollback(path) {
    if(confirm("Overwrite live data with this snapshot?")) {
        await fetch(`${API}/snapshots/rollback?job_id=${currentJobId}&path=${encodeURIComponent(path)}`);
        alert("Rollback triggered check logs");
        loadLogs();
    }
}

export async function purgeAll() {
    if(prompt("Type DELETE") === "DELETE") {
        await fetch(`${API}/action/purge_all`);
        loadLogs();
    }
}
