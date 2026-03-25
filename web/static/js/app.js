const API_BASE = '';

async function fetchStats() {
    try {
        const res = await fetch(`${API_BASE}/api/stats`);
        const json = await res.json();
        const data = json.data;

        document.getElementById('total-slots').textContent = data.total_slots;
        document.getElementById('healthy-slots').textContent = data.healthy_slots;
        document.getElementById('active-connections').textContent = data.active_connections;

        const tbody = document.getElementById('slots-body');
        tbody.innerHTML = '';

        const slots = data.slot_stats || [];
        slots.sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true }));

        for (const slot of slots) {
            const tr = document.createElement('tr');
            tr.className = slot.status === 'healthy' ? 'row-healthy' : 'row-unhealthy';
            tr.innerHTML = `
                <td>${slot.name}</td>
                <td>${slot.ipv6_address || '-'}</td>
                <td>${slot.public_ipv4 || '-'}</td>
                <td><span class="badge badge-${slot.status}">${slot.status}</span></td>
                <td>${slot.active_connections}</td>
                <td>${slot.last_checked_at ? new Date(slot.last_checked_at).toLocaleTimeString() : '-'}</td>
            `;
            tbody.appendChild(tr);
        }
    } catch (err) {
        console.error('Failed to fetch stats:', err);
    }
}

async function fetchHealth() {
    try {
        const res = await fetch(`${API_BASE}/api/health`);
        const json = await res.json();
        const data = json.data;

        const badge = document.getElementById('health-badge');
        badge.textContent = data.status;
        badge.className = `badge badge-${data.status}`;
    } catch (err) {
        const badge = document.getElementById('health-badge');
        badge.textContent = 'offline';
        badge.className = 'badge badge-unhealthy';
    }
}

function refresh() {
    fetchStats();
    fetchHealth();
}

refresh();
setInterval(refresh, 5000);