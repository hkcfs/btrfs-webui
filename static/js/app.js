import * as Config from './config.js';
import * as Jobs from './jobs.js';
import * as Logs from './logs.js';
import * as Browser from './browser.js';
import * as Dashboard from './dashboard.js';
import * as Utils from './utils.js';

window.nav = (view) => {
    document.querySelectorAll('.view-section').forEach(el => el.style.display = 'none');
    document.querySelectorAll('.nav-btn').forEach(el => el.classList.remove('active'));
    
    document.getElementById(`view-${view}`).style.display = 'block';
    
    const btn = Array.from(document.querySelectorAll('.nav-btn')).find(b => b.innerText.toLowerCase().includes(view.split(' ')[0]));
    if(btn) btn.classList.add('active');

    document.getElementById('pageTitle').innerText = view.charAt(0).toUpperCase() + view.slice(1);

    if(view === 'jobs') Jobs.renderJobs();
    if(view === 'dashboard') Dashboard.loadDashboard();
    if(view === 'logs') Logs.loadLogs();
};

// --- GLOBAL EXPORTS (The fix for "toggleTheme is not defined") ---
window.toggleTheme = Utils.toggleTheme;
window.logout = Utils.logout;
window.closeModal = Utils.closeModal;

// Jobs
window.editJob = Jobs.editJob;
window.runJob = Jobs.runJob;
window.viewSnapshots = Jobs.viewSnapshots;
window.deleteJob = Jobs.deleteJob;
window.toggleJobRetentionUI = Jobs.toggleJobRetentionUI;
window.selectSnap = Jobs.selectSnap;
window.compareSnapshots = Jobs.compareSnapshots;
window.delSnap = Jobs.delSnap;
window.rollback = Jobs.rollback;
window.purgeAll = Jobs.purgeAll;

// Dashboard
window.doAction = Dashboard.doAction;
window.runSmart = Dashboard.runSmart;
window.loadStorage = Dashboard.loadDashboard;

// Browser
window.browseUp = Browser.browseUp;
window.loadSnapshotFiles = Browser.loadSnapshotFiles;
window.browse = Browser.browse;
window.download = Browser.download;

// Logs
window.clearLogs = Logs.clearLogs;

// Init
setInterval(() => document.getElementById('clock').innerText = new Date().toLocaleTimeString(), 1000);
Config.loadConfig().then(() => window.nav('dashboard'));

document.getElementById('jobForm').onsubmit = Jobs.saveJobForm;
document.getElementById('globalForm').onsubmit = Config.saveGlobalConfig;
