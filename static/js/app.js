import * as Config from './config.js';
import * as Jobs from './jobs.js';
import * as Logs from './logs.js';
import * as Browser from './browser.js';
import * as Dashboard from './dashboard.js';
import * as Utils from './utils.js';
import * as Calendar from './calendar.js';

// --- Router ---
window.nav = (view) => {
    try {
        document.querySelectorAll('.view-section').forEach(el => el.style.display = 'none');
        document.querySelectorAll('.nav-btn').forEach(el => el.classList.remove('active'));
        
        const target = document.getElementById(`view-${view}`);
        if (target) target.style.display = 'block';
        
        const btn = Array.from(document.querySelectorAll('.nav-btn')).find(b => b.innerText.toLowerCase().includes(view.split(' ')[0]));
        if(btn) btn.classList.add('active');

        const titleEl = document.getElementById('pageTitle');
        if(titleEl) titleEl.innerText = view.charAt(0).toUpperCase() + view.slice(1);

        if(view === 'jobs') Jobs.renderJobs();
        if(view === 'dashboard') {
            Dashboard.loadDashboard();
            Calendar.initCalendar();
        }
        if(view === 'logs') Logs.loadLogs();
    } catch(e) {
        console.error("Navigation error:", e);
    }
};

// --- GLOBAL EXPORTS ---
// This assigns the imported functions to the window object so HTML onclick="" works
window.logout = Utils.logout;
window.toggleTheme = Utils.toggleTheme;
window.closeModal = Utils.closeModal;
window.doAction = Dashboard.doAction;
window.runSmart = Dashboard.runSmart;
window.loadStorage = Dashboard.loadDashboard;
window.editJob = Jobs.editJob;
window.saveJobForm = Jobs.saveJobForm;
window.runJob = Jobs.runJob;
window.deleteJob = Jobs.deleteJob;
window.toggleJobRetentionUI = Jobs.toggleJobRetentionUI;
window.toggleJobSchedUI = Jobs.toggleJobSchedUI;
window.viewSnapshots = Jobs.viewSnapshots;
window.toggleLock = Jobs.toggleLock;
window.selectSnap = Jobs.selectSnap;
window.compareSnapshots = Jobs.compareSnapshots;
window.delSnap = Jobs.delSnap;
window.rollback = Jobs.rollback;
window.purgeAll = Jobs.purgeAll;
window.browse = Browser.browse;
window.browseUp = Browser.browseUp;
window.download = Browser.download;
window.loadSnapshotFiles = Browser.loadSnapshotFiles;
window.exportConfig = Config.exportConfig;
window.importConfig = Config.importConfig;
window.clearLogs = Logs.clearLogs;
window.openCalendar = Calendar.openCalendarModal;
window.changeMonth = Calendar.changeMonth;

// --- INIT ---
// We wrap this to catch errors that stop the "Loading..." from disappearing
async function startApp() {
    try {
        setInterval(() => {
            const el = document.getElementById('clock');
            if(el) el.innerText = new Date().toLocaleTimeString();
        }, 1000);

        await Config.loadConfig();
        window.nav('dashboard');
        console.log("App initialized successfully");
    } catch(e) {
        console.error("App init failed:", e);
        alert("Failed to load application. Check console for details.");
    }
}

// Bind Forms
const jForm = document.getElementById('jobForm');
if(jForm) jForm.onsubmit = Jobs.saveJobForm;

const gForm = document.getElementById('globalForm');
if(gForm) gForm.onsubmit = Config.saveGlobalConfig;

startApp();
