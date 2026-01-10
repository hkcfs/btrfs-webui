import * as Utils from './utils.js';
import * as Config from './config.js';
import * as Dashboard from './dashboard.js';
import * as Jobs from './jobs.js';
import * as Logs from './logs.js';
import * as Browser from './browser.js';

// Expose functions to window for onclick handlers in HTML
window.switchTab = (id) => {
    document.querySelectorAll('.tab-content').forEach(e => e.classList.remove('active'));
    document.querySelectorAll('.tab-btn').forEach(e => e.classList.remove('active'));
    document.getElementById(id).classList.add('active');
    // We can't access event.target easily from here unless passed, so we skip the active class on btn for now or query selector
    if(id === 'dashboard') Dashboard.loadDashboard();
    if(id === 'jobs') Jobs.renderJobs();
};

window.closeModal = Utils.closeModal;
window.toggleTheme = Utils.toggleTheme;
window.loadStorage = Dashboard.loadDashboard; // Reloads all dash
window.loadBtrfsStats = Dashboard.loadDashboard;
window.runSmart = Dashboard.runSmart;
window.doAction = Dashboard.doAction;
window.clearLogs = Logs.clearLogs;
window.editJob = Jobs.editJob;
window.runJob = Jobs.runJob;
window.viewSnapshots = Jobs.viewSnapshots;
window.browse = Browser.browse;
window.browseUp = Browser.browseUp;
window.download = Browser.download;
window.delSnap = Jobs.delSnap;
window.rollback = Jobs.rollback;
window.purgeAll = Jobs.purgeAll;
window.deleteJob = Jobs.deleteJob;
window.toggleJobRetentionUI = Jobs.toggleJobRetentionUI;
window.selectSnap = Jobs.selectSnap;
window.compareSnapshots = Jobs.compareSnapshots;
window.logout = () => { document.cookie = "session_token=; expires=Thu, 01 Jan 1970 00:00:00 UTC; path=/;"; window.location.reload(); };

// Listeners
document.getElementById('jobForm').onsubmit = Jobs.saveJobForm;
document.getElementById('globalForm').onsubmit = async (e) => { e.preventDefault(); await Config.saveConfig(); };

// Init
setInterval(() => document.getElementById('clock').innerText = new Date().toLocaleTimeString(), 1000);
Config.loadConfig();
setInterval(Dashboard.loadDashboard, 10000);
