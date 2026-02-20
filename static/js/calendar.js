import { API, openModal } from './utils.js';

let currentMonth = new Date();
let cachedLogs = [];

export async function initCalendar() {
    try {
        const res = await fetch(`${API}/history`);
        cachedLogs = await res.json() || [];
        renderMiniWidget();
    } catch(e) { 
        console.error("Calendar error:", e);
        const el = document.getElementById('calMiniList');
        if(el) el.innerHTML = "Failed to load history.";
    }
}

// Shows last 5 days on Dashboard with BADGES
function renderMiniWidget() {
    const container = document.getElementById('calMiniList');
    if(!container) return;

    const grouped = groupLogsByDate(cachedLogs);
    const sortedDates = Object.keys(grouped).sort().reverse().slice(0, 5);

    if(sortedDates.length === 0) {
        container.innerHTML = "<div style='padding:15px; opacity:0.5; text-align:center; border:1px dashed #333'>No activity recorded yet.</div>";
        return;
    }

    container.innerHTML = sortedDates.map(date => {
        const dayLogs = grouped[date];
        const count = dayLogs.length;
        
        // Determine Status
        const hasErrors = dayLogs.some(l => l.status.toLowerCase().includes("fail"));
        const hasMissed = dayLogs.some(l => l.status.toLowerCase().includes("missed"));
        
        let badgeClass = "badge-success";
        let badgeText = `✔ OK (${count})`;
        
        if (hasErrors) {
            badgeClass = "badge-fail";
            badgeText = `⚠ ERRORS (${count})`;
        } else if (hasMissed) {
            badgeClass = "badge-warn";
            badgeText = `⚠ MISSED (${count})`;
        }
        
        // Format Date (e.g. "Jan 24")
        const dObj = new Date(date);
        const dayName = dObj.toLocaleDateString(undefined, { weekday: 'short' }); // "Sat"
        const dateStr = dObj.toLocaleDateString(undefined, { month: 'short', day: 'numeric' }); // "Jan 24"

        return `
        <div style="display:flex; justify-content:space-between; align-items:center; padding:10px 0; border-bottom:1px solid var(--border);">
            <div style="display:flex; gap:10px; align-items:center;">
                <span style="color:var(--text-muted); font-size:0.8rem; width:30px">${dayName}</span>
                <span style="font-weight:600; color:var(--text-main)">${dateStr}</span>
            </div>
            <span class="badge ${badgeClass}">${badgeText}</span>
        </div>`;
    }).join('');
}

export function openCalendarModal() {
    openModal('calendarModal');
    // Ensure we render the current real month first, not a cached one
    currentMonth = new Date();
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

    const grouped = groupLogsByDate(cachedLogs);

    // 1. Render Headers
    ['SUN', 'MON', 'TUE', 'WED', 'THU', 'FRI', 'SAT'].forEach(d => {
        grid.innerHTML += `<div class="cal-day-header">${d}</div>`;
    });

    // 2. Render Empty Cells (before 1st of month)
    for(let i=0; i<startDay; i++) {
        grid.innerHTML += `<div style="background:var(--bg-app); border-right:1px solid var(--border); border-bottom:1px solid var(--border)"></div>`;
    }

    // 3. Render Days
    const now = new Date();
    const todayStr = `${now.getFullYear()}-${String(now.getMonth()+1).padStart(2,'0')}-${String(now.getDate()).padStart(2,'0')}`;

    for(let d=1; d<=daysInMonth; d++) {
        const dateStr = `${year}-${String(month+1).padStart(2,'0')}-${String(d).padStart(2,'0')}`;
        const isToday = dateStr === todayStr ? 'cal-today' : '';
        const dayLogs = grouped[dateStr] || [];
        
        let dots = "";
        dayLogs.forEach(l => {
            let color = 'bg-success';
            if (l.status.toLowerCase().includes("fail")) color = 'bg-fail';
            else if (l.status.toLowerCase().includes("missed")) color = 'bg-warn';
            dots += `<span class="cal-event-dot ${color}" title="${l.type}: ${l.status}"></span>`;
        });

        grid.innerHTML += `
        <div class="cal-day ${isToday}">
            <span class="cal-day-num">${d}</span>
            <div class="cal-dots">${dots}</div>
        </div>`;
    }
}

function groupLogsByDate(logs) {
    const groups = {};
    logs.forEach(l => {
        let dateStr = "";

        if(l.timestamp) {
            if(l.timestamp.includes("T")) {
                dateStr = l.timestamp.split("T")[0];
            } else {
                // Try to parse known formats
                const isoMatch = l.timestamp.match(/(\d{4})-(\d{2})-(\d{2})/);
                const dmyMatch = l.timestamp.match(/(\d{2})-(\d{2})-(\d{4})/);

                if(isoMatch) {
                    dateStr = `${isoMatch[1]}-${isoMatch[2]}-${isoMatch[3]}`;
                } else if(dmyMatch) {
                    dateStr = `${dmyMatch[3]}-${dmyMatch[2]}-${dmyMatch[1]}`;
                }
            }
        }
        
        if(dateStr.length === 10) {
            if(!groups[dateStr]) groups[dateStr] = [];
            groups[dateStr].push(l);
        }
    });
    return groups;
}
