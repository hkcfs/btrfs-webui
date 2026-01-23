import { API, openModal } from './utils.js';

let currentMonth = new Date();
let cachedLogs = [];

export async function initCalendar() {
    try {
        const res = await fetch(`${API}/history`);
        cachedLogs = await res.json() || [];
        renderMiniWidget();
    } catch(e) { console.error(e); }
}

// Shows last 5 days on Dashboard
function renderMiniWidget() {
    const container = document.getElementById('calMiniList');
    if(!container) return;

    // Group logs by date
    const grouped = groupLogsByDate(cachedLogs);
    const sortedDates = Object.keys(grouped).sort().reverse().slice(0, 5);

    if(sortedDates.length === 0) {
        container.innerHTML = "<div style='padding:10px; opacity:0.6'>No history yet.</div>";
        return;
    }

    container.innerHTML = sortedDates.map(date => {
        const dayLogs = grouped[date];
        // Determine status for the day (Fail > Warn > Success)
        let statusHtml = `<span class="text-success">OK (${dayLogs.length})</span>`;
        if(dayLogs.some(l => l.status.includes("Failed"))) statusHtml = `<span class="text-fail">ERRORS</span>`;
        
        // Format Date nicely (e.g. "Jan 24")
        const dObj = new Date(date);
        const dateStr = dObj.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });

        return `
        <div class="cal-mini-row">
            <span class="cal-date">${dateStr}</span>
            <span class="cal-status">${statusHtml}</span>
        </div>`;
    }).join('');
}

// Opens Full Modal
export function openCalendarModal() {
    openModal('calendarModal');
    renderFullCalendar();
}

export function changeMonth(delta) {
    currentMonth.setMonth(currentMonth.getMonth() + delta);
    renderFullCalendar();
}

function renderFullCalendar() {
    const grid = document.getElementById('calGrid');
    const label = document.getElementById('calMonthLabel');
    if(!grid) return;

    grid.innerHTML = "";
    
    const year = currentMonth.getFullYear();
    const month = currentMonth.getMonth();
    
    label.innerText = currentMonth.toLocaleDateString(undefined, { month: 'long', year: 'numeric' });

    const firstDay = new Date(year, month, 1);
    const lastDay = new Date(year, month + 1, 0);
    const daysInMonth = lastDay.getDate();
    const startDay = firstDay.getDay(); // 0 = Sun

    // Group logs
    const grouped = groupLogsByDate(cachedLogs);

    // Headers
    ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'].forEach(d => {
        grid.innerHTML += `<div class="cal-day-header">${d}</div>`;
    });

    // Empty cells
    for(let i=0; i<startDay; i++) {
        grid.innerHTML += `<div class="cal-day" style="border:none; background:transparent"></div>`;
    }

    // Days
    const todayStr = new Date().toISOString().split('T')[0];

    for(let d=1; d<=daysInMonth; d++) {
        // Construct YYYY-MM-DD
        const dateStr = `${year}-${String(month+1).padStart(2,'0')}-${String(d).padStart(2,'0')}`;
        const isToday = dateStr === todayStr ? 'cal-today' : '';
        const dayLogs = grouped[dateStr] || [];
        
        let dots = "";
        dayLogs.forEach(l => {
            const color = l.status.includes("Failed") ? 'bg-fail' : 'bg-success';
            dots += `<span class="cal-event-dot ${color}" title="${l.type}: ${l.status}"></span>`;
        });

        grid.innerHTML += `
        <div class="cal-day ${isToday}">
            <span class="cal-day-num">${d}</span>
            <div style="margin-top:20px; display:flex; flex-wrap:wrap">${dots}</div>
        </div>`;
    }
}

function groupLogsByDate(logs) {
    const groups = {};
    logs.forEach(l => {
        // Go timestamp is usually ISO start (2026-01-24T...)
        // But your legacy custom format might be 24-01-2026...
        // Let's try basic ISO split first
        let dateStr = "";
        if(l.timestamp.includes("T")) {
            dateStr = l.timestamp.split("T")[0];
        } else {
            // Fallback for custom format (DD-MM-YYYY...) hacky parse
            // Assuming 18-01-2026...
            const parts = l.timestamp.split('-');
            if(parts.length >= 3) dateStr = `${parts[2]}-${parts[1]}-${parts[0]}`; 
        }
        
        // Normalize
        if(dateStr.length === 10) {
            if(!groups[dateStr]) groups[dateStr] = [];
            groups[dateStr].push(l);
        }
    });
    return groups;
}
