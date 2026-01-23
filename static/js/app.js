import * as Config from './config.js';
import * as Jobs from './jobs.js';
import * as Logs from './logs.js';
import * as Browser from './browser.js';
import * as Dashboard from './dashboard.js';
import * as Utils from './utils.js';
import * as Calendar from './calendar.js';

// --- Router ---
window.nav = (view) => {
    // 1. Hide all views
    document.querySelectorAll('.view-section').forEach(el => el.style.display = 'none');
    document.querySelectorAll('.nav-btn').forEach(el => el.classList.remove('active'));
    
    // 2. Show selected view
    const target = document.getElementById(`view-${view}`);
    if (target) target.style.display = 'block';
    
    // 3. Highlight Sidebar
    const btn = Array.from(document.querySelectorAll('.nav-btn')).find(b => b.innerText.toLowerCase().includes(view.split(' ')[0]));
    if(btn) btn.classList.add('active');

    // 4. Update Title
    document.getElementById('pageTitle').innerText = view.charAt(0).toUpperCase() + view.slice(1);

    // 5. Trigger View-Specific Logic
    if(view === 'jobs') {
        Jobs.renderJobs();
    }
    if(view === 'dashboard') {
        Dashboard.loadDashboard();
        Calendar.initCalendar();
    }
    // Note: 'Settings' view doesn't need load logic on switch
};

// --- GLOBAL EXPORTS (Required for HTML onclick="") ---

// Utils / Auth
window.logout = Utils.logout;
window.toggleTheme = Utils.toggleTheme;
window.closeModal = Utils.closeModal;

// Dashboard Actions
window.doAction = Dashboard.doAction;
window.runSmart = Dashboard.runSmart;
window.loadStorage = Dashboard.loadDashboard;

// Job Management
window.editJob = Jobs.editJob;
window.saveJobForm = Jobs.saveJobForm; // Explicitly export form handler
window.runJob = Jobs.runJob;
window.deleteJob = Jobs.deleteJob;
window.toggleJobRetentionUI = Jobs.toggleJobRetentionUI;
window.toggleJobSchedUI = Jobs.toggleJobSchedUI;

// Snapshots & Browser
window.viewSnapshots = Jobs.viewSnapshots;
window.selectSnap = Jobs.selectSnap;
window.compareSnapshots = Jobs.compareSnapshots;
window.delSnap = Jobs.delSnap;
window.rollback = Jobs.rollback;
window.purgeAll = Jobs.purgeAll;
window.browse = Browser.browse;
window.browseUp = Browser.browseUp;
window.download = Browser.download;
window.loadSnapshotFiles = Browser.loadSnapshotFiles;

// Logs
window.clearLogs = Logs.clearLogs;

// Calendar
window.openCalendar = Calendar.openCalendarModal;
window.changeMonth = Calendar.changeMonth;

// --- INITIALIZATION ---

// 1. Clock
setInterval(() => {
    const el = document.getElementById('clock');
    if(el) el.innerText = new Date().toLocaleTimeString();
}, 1000);

// 2. Load Config & Start
Config.loadConfig().then(() => {
    window.nav('dashboard');
});

// 3. Attach Listeners (Backup for inline onclicks)
const jobForm = document.getElementById('jobForm');
if(jobForm) jobForm.onsubmit = Jobs.saveJobForm;

const globalForm = document.getElementById('globalForm');
if(globalForm) globalForm.onsubmit = Config.saveGlobalConfig;
