import { API, openModal, closeModal } from './utils.js';
import { appConfig, saveConfig } from './config.js';
import { loadLogs, pollLog } from './logs.js';
import { browse, download } from './browser.js';

let currentJobId = "";
let selectedSnaps = [];

export function renderJobs() {
    const list = document.getElementById('jobList');
    if(!list) return;
    
    let html = "";
    
    if(appConfig.jobs) {
        html += appConfig.jobs.map((j, i) => {
            // Display Logic for Cron vs Interval
            let sched = "Disabled";
            if(j.schedule.enabled) {
                if(j.schedule.type === 'cron') sched = `Cron: ${j.schedule.value}`;
                else sched = `Every ${j.schedule.value} ${j.schedule.unit}`;
            }
            
            setTimeout(() => loadJobStats(j.id), 200);

            return `
            <div class="card">
                <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:20px;">
                    <div style="font-size:1.1rem; font-weight:700; color:var(--text-main)">
                        <span style="color:var(--accent)">//</span> ${j.name}
                    </div>
                    <div style="width:8px; height:8px; background:var(--success); border-radius:50%; box-shadow:0 0 5px var(--success)"></div>
                </div>
                
                <div style="font-family:monospace; color:var(--text-muted); font-size:0.85rem; margin-bottom:20px; flex:1">
                    <div style="margin-bottom:5px">SRC: ${shortPath(j.source)}</div>
                    <div style="margin-bottom:5px">DST: ${shortPath(j.dest)}</div>
                    <div>SCH: ${sched}</div>
                </div>

                <div id="stats-${j.id}" style="border-top:1px solid var(--border); padding-top:15px; margin-bottom:15px; display:flex; justify-content:space-between; font-size:0.8rem; color:var(--text-muted)">
                    <span>Loading...</span>
                </div>

                <div class="flex">
                    <button class="btn btn-primary" style="flex:1" onclick="window.runJob('${j.id}')">Run</button>
                    <button class="btn" style="flex:1" onclick="window.viewSnapshots('${j.id}')">Snaps</button>
                    <button class="btn" onclick="window.editJob(${i})">⚙</button>
                </div>
                <span class="card-inner"></span>
            </div>
            `;
        }).join('');
    }

    html += `
        <div class="card" style="border-style:dashed; align-items:center; justify-content:center; cursor:pointer; min-height:250px; opacity:0.6; transition:0.2s" onclick="window.editJob('new')" onmouseover="this.style.opacity=1" onmouseout="this.style.opacity=0.6">
            <div style="font-size:3rem; font-weight:200; color:var(--text-muted)">+</div>
            <div style="margin-top:10px; text-transform:uppercase; letter-spacing:1px">New Job</div>
        </div>
    `;

    list.innerHTML = html;
}

function shortPath(path) {
    if(!path) return "";
    return path.length > 20 ? '...'+path.slice(-20) : path;
}

async function loadJobStats(jobId) {
    try {
        const res = await fetch(`${API}/snapshots/stats?job_id=${jobId}`);
        if(res.ok) {
            const stats = await res.json();
            const el = document.getElementById(`stats-${jobId}`);
            if(el) {
                el.innerHTML = `
                    <span>TOTAL: <b style="color:var(--text-main)">${stats.count}</b></span>
                    <span>OLDEST: <b style="color:var(--text-main)">${stats.oldest}</b></span>
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
        toggleJobSchedUI();
    } else {
        const j = appConfig.jobs[idx];
        document.getElementById('j_id').value = j.id;
        document.getElementById('j_name').value = j.name;
        document.getElementById('j_src').value = j.source;
        document.getElementById('j_dest').value = j.dest;
        
        // Schedule Config
        document.getElementById('j_sched_en').checked = j.schedule.enabled;
        document.getElementById('j_sched_type').value = j.schedule.type || 'every_x';
        if(j.schedule.type === 'cron') {
            document.getElementById('j_sched_cron').value = j.schedule.value;
        } else {
            document.getElementById('j_sched_val').value = j.schedule.value;
            document.getElementById('j_sched_unit').value = j.schedule.unit;
        }
        
        // Retention
        document.getElementById('j_ret_en').checked = j.retention.enabled;
        document.getElementById('j_ret_mode').value = j.retention.mode;
        document.getElementById('j_ret_val').value = j.retention.value;
        document.getElementById('j_ret_unit').value = j.retention.unit;
        
        // Advanced
        document.getElementById('j_pre').value = j.pre_command || "";
        document.getElementById('j_post').value = j.post_command || "";
        document.getElementById('j_repl_en').checked = j.replication?.enabled || false;
        document.getElementById('j_repl_dest').value = j.replication?.target_dest || "";
        
        toggleJobRetentionUI();
        toggleJobSchedUI();
    }
}

// Global exports for UI onclicks
window.toggleJobSchedUI = () => {
    const type = document.getElementById('j_sched_type').value;
    if(type === 'cron') {
        document.getElementById('sched_interval_ui').style.display = 'none';
        document.getElementById('sched_cron_ui').style.display = 'block';
    } else {
        document.getElementById('sched_interval_ui').style.display = 'flex';
        document.getElementById('sched_cron_ui').style.display = 'none';
    }
}

export function saveJobForm(e) {
    e.preventDefault();
    const id = document.getElementById('j_id').value;
    const schedType = document.getElementById('j_sched_type').value;
    
    // Determine Schedule Value based on Type
    let schedVal = document.getElementById('j_sched_val').value;
    if(schedType === 'cron') schedVal = document.getElementById('j_sched_cron').value;

    const job = {
        id: id === "new" ? Date.now().toString() : id,
        name: document.getElementById('j_name').value,
        source: document.getElementById('j_src').value,
        dest: document.getElementById('j_dest').value,
        schedule: {
            enabled: document.getElementById('j_sched_en').checked,
            type: schedType,
            value: schedVal,
            unit: document.getElementById('j_sched_unit').value, // irrelevant if cron
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

    if(id === "new") { if(!appConfig.jobs) appConfig.jobs=[]; appConfig.jobs.push(job); } 
    else { const i = appConfig.jobs.findIndex(x=>x.id===id); if(i>-1) appConfig.jobs[i]=job; }
    
    saveConfig();
    closeModal('jobModal');
}

export function deleteJob() {
    if(!confirm("Delete Job?")) return;
    const id = document.getElementById('j_id').value;
    appConfig.jobs = appConfig.jobs.filter(x => x.id !== id);
    saveConfig(); closeModal('jobModal');
}

export function toggleJobRetentionUI() {
    const isTime = document.getElementById('j_ret_mode').value === 'time';
    const el = document.getElementById('j_ret_unit');
    el.disabled = !isTime;
    el.style.opacity = isTime ? 1 : 0.3;
}

export async function runJob(id) {
    if(!confirm("Run job?")) return;
    await fetch(`${API}/action/job?id=${id}`);
    loadLogs();
}

export async function viewSnapshots(jobId) {
    currentJobId = jobId; selectedSnaps = [];
    openModal('snapListModal');
    document.getElementById('snapListContainer').innerHTML = "<div style='padding:20px;text-align:center'>Loading...</div>";

    const res = await fetch(`${API}/snapshots/list?job_id=${jobId}`);
    const list = await res.json();
    
    if(list.length === 0) {
        document.getElementById('snapListContainer').innerHTML = "<div style='padding:20px;text-align:center'>No snapshots found.</div>";
        return;
    }

    const rows = list.map(s => `
        <tr class="snap-row">
            <td style="width:30px; text-align:center;"><input type="checkbox" onchange="window.selectSnap(this, '${s.path}')"></td>
            <td>
                <div style="font-weight:600; font-size:0.95rem;">${s.date}</div>
            </td>
            <td style="font-family:monospace; font-size:0.8rem; opacity:0.6; overflow:hidden; text-overflow:ellipsis; max-width:200px;">
                ${s.name}
            </td>
            <td style="text-align:right;">
                <button class="btn btn-sm btn-sec" onclick="window.rollback('${s.path}')" title="Restore">♻️</button>
                <button class="btn btn-sm btn-sec" onclick="window.browse('${s.path}')" title="Browse">📂</button>
                <button class="btn btn-sm btn-danger" onclick="window.delSnap('${s.path}')" title="Delete">🗑</button>
            </td>
        </tr>
    `).join('');

    document.getElementById('snapListContainer').innerHTML = `
        <table style="width:100%; border-collapse:collapse;">
            <thead style="background:var(--bg-app); text-align:left; font-size:0.85rem; color:var(--text-main);">
                <tr>
                    <th style="padding:10px; border-bottom:1px solid var(--border);"></th>
                    <th style="padding:10px; border-bottom:1px solid var(--border);">Created</th>
                    <th style="padding:10px; border-bottom:1px solid var(--border);">Folder Name</th>
                    <th style="padding:10px; border-bottom:1px solid var(--border); text-align:right">Actions</th>
                </tr>
            </thead>
            <tbody>${rows}</tbody>
        </table>
    `;
}

export function selectSnap(cb, path) { if(cb.checked) selectedSnaps.push(path); else selectedSnaps = selectedSnaps.filter(p => p !== path); }
export async function compareSnapshots() {
    if(selectedSnaps.length !== 2) { alert("Select exactly 2 snapshots."); return; }
    const res = await fetch(`${API}/snapshots/diff?a=${encodeURIComponent(selectedSnaps[0])}&b=${encodeURIComponent(selectedSnaps[1])}`);
    pollLog((await res.json()).id);
}
export async function delSnap(path) { if(confirm("Delete?")) { await fetch(`${API}/snapshots/delete?path=${encodeURIComponent(path)}`); viewSnapshots(currentJobId); loadJobStats(currentJobId); } }
export async function rollback(path) { if(confirm("Overwrite live data?")) { await fetch(`${API}/snapshots/rollback?job_id=${currentJobId}&path=${encodeURIComponent(path)}`); alert("Rollback triggered"); loadLogs(); } }
export async function purgeAll() { if(prompt("Type DELETE") === "DELETE") { await fetch(`${API}/action/purge_all`); loadLogs(); } }
