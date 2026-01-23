import { API, openModal } from './utils.js';

let currentMonth = new Date();
let cachedLogs = [];

export async function initCalendar() {
    try {
        const res = await fetch(`${API}/history`);
        cachedLogs = await res.json() || [];
        renderMiniWidget();
    } catch(e) { 
        console.error("Calendar init failed:", e);
        document.getElementById('calMiniList').innerHTML = "<small>Failed to load history</small>";
    }
}

// Shows last 5 days on Dashboard
function renderMiniWidget() {
    const container = document.getElementById('calMiniList');
    if(!container) return;

    const grouped = groupLogsByDate(cachedLogs);
    const sortedDates = Object.keys(grouped).sort().reverse().slice(0, 5);

    if(sortedDates.length === 0) {
        container.innerHTML = "<div style='padding:10px; opacity:0.6; font-size:0.85rem'>No history recorded yet.</div>";
        return;
    }

    container.innerHTML = sortedDates.map(date => {
        const dayLogs = grouped[date];
        let statusClass = "text-success";
        let statusText = `OK (${dayLogs.length})`;
        
        if(dayLogs.some(l => l.status.toLowerCase().includes("fail"))) {
            statusClass = "text-fail";
            statusText = "ERRORS";
        }
        
        const dObj = new Date(date);
        const dateStr = dObj.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });

        return `
        <div style="display:flex; justify-content:space-between; padding:6px 0; border-bottom:1px solid #333; font-size:0.9rem">
            <span style="color:#e0e0e0">${dateStr}</span>
            <span class="${statusClass}" style="font-weight:bold; font-size:0.8rem">${statusText}</span>
        </div>`;
    }).join('');
}

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
    const startDay = firstDay.getDay();

    const grouped = groupLogsByDate(cachedLogs);

    // Headers
    ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'].forEach(d => {
        grid.innerHTML += `<div style="text-align:center; color:#888; font-size:0.8rem; padding-bottom:5px;">${d}</div>`;
    });

    // Empty cells
    for(let i=0; i<startDay; i++) {
        grid.innerHTML += `<div></div>`;
    }

    // Days
    const todayStr = new Date().toISOString().split('T')[0];

    for(let d=1; d<=daysInMonth; d++) {
        const dateStr = `${year}-${String(month+1).padStart(2,'0')}-${String(d).padStart(2,'0')}`;
        const isToday = dateStr === todayStr ? 'border-color: #ff4d00; background: rgba(255, 77, 0, 0.1);' : '';
        const dayLogs = grouped[dateStr] || [];
        
        let dots = "";
        dayLogs.forEach(l => {
            const color = l.status.toLowerCase().includes("fail") ? '#ff3333' : '#00c853';
            dots += `<div style="width:6px; height:6px; border-radius:50%; background:${color}; margin:1px;"></div>`;
        });

        grid.innerHTML += `
        <div style="background:#0a0a0a; border:1px solid #333; min-height:70px; padding:5px; position:relative; ${isToday}">
            <span style="position:absolute; top:5px; right:5px; font-size:0.8rem; color:#888">${d}</span>
            <div style="margin-top:20px; display:flex; flex-wrap:wrap; gap:2px">${dots}</div>
        </div>`;
    }
}

function groupLogsByDate(logs) {
    const groups = {};
    logs.forEach(l => {
        let dateStr = "";
        // Handle ISO (Go default) or Custom format
        if(l.timestamp && l.timestamp.includes("T")) {
            dateStr = l.timestamp.split("T")[0];
        } else if (l.timestamp) {
            // Try DD-MM-YYYY
            const parts = l.timestamp.split('-');
            if(parts.length >= 3) dateStr = `${parts[2]}-${parts[1]}-${parts[0]}`; 
        }
        
        if(dateStr.length === 10) {
            if(!groups[dateStr]) groups[dateStr] = [];
            groups[dateStr].push(l);
        }
    });
    return groups;
}
