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
                <td class="actions-cell">
                    <button class="btn-changeip" onclick="changeIP('${slot.name}')" ${slot.status !== 'healthy' ? 'disabled' : ''}>Change IP</button>
                    <button class="btn btn-sm btn-delete" onclick="deleteSlot('${slot.name}')">Delete</button>
                </td>
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

function setStatus(msg, isError) {
    const el = document.getElementById('provision-status');
    el.textContent = msg;
    el.style.color = isError ? '#f85149' : '#58a6ff';
}

async function provisionSlots() {
    const iface = document.getElementById('provision-interface').value || 'usb0';
    const slots = parseInt(document.getElementById('provision-slots').value) || 5;
    const btn = document.getElementById('btn-provision');

    btn.disabled = true;
    btn.textContent = 'Provisioning...';
    setStatus(`Creating ${slots} slots on ${iface}...`, false);

    try {
        const res = await fetch(`${API_BASE}/api/provision`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ interface: iface, slots: slots })
        });
        const json = await res.json();

        if (!res.ok) {
            setStatus(`Provision failed: ${json.errors || res.statusText}`, true);
            return;
        }

        const d = json.data;
        setStatus(`Done: ${d.created} created, ${d.failed} failed, ${d.total} total`, false);
        refresh();
    } catch (err) {
        setStatus(`Provision error: ${err.message}`, true);
    } finally {
        btn.disabled = false;
        btn.textContent = 'Provision';
    }
}

async function teardownAll() {
    if (!confirm('Destroy ALL slots? Active connections will be dropped.')) return;

    const btn = document.getElementById('btn-teardown');
    btn.disabled = true;
    btn.textContent = 'Tearing down...';
    setStatus('Destroying all slots...', false);

    try {
        const res = await fetch(`${API_BASE}/api/teardown`, { method: 'POST' });
        const json = await res.json();

        if (!res.ok) {
            setStatus(`Teardown failed: ${json.errors || res.statusText}`, true);
            return;
        }

        const d = json.data;
        setStatus(`Teardown complete: ${d.total} destroyed, ${d.failed} failed`, false);
        refresh();
    } catch (err) {
        setStatus(`Teardown error: ${err.message}`, true);
    } finally {
        btn.disabled = false;
        btn.textContent = 'Teardown All';
    }
}

async function deleteSlot(slotName) {
    if (!confirm(`Delete ${slotName}?`)) return;

    try {
        const res = await fetch(`${API_BASE}/api/slots/${slotName}`, { method: 'DELETE' });
        const json = await res.json();

        if (!res.ok) {
            alert(`Delete failed: ${json.errors || res.statusText}`);
            return;
        }

        refresh();
    } catch (err) {
        alert(`Delete error: ${err.message}`);
    }
}

async function changeIP(slotName) {
    const btn = document.querySelector(`button[onclick="changeIP('${slotName}')"]`);
    if (btn) {
        btn.disabled = true;
        btn.textContent = 'Changing...';
    }

    try {
        const res = await fetch(`${API_BASE}/api/slots/${slotName}/changeip`, { method: 'POST' });
        const json = await res.json();

        if (!res.ok) {
            alert(`Failed: ${json.errors || res.statusText}`);
            return;
        }

        refresh();
    } catch (err) {
        alert(`Change IP failed: ${err.message}`);
    } finally {
        if (btn) {
            btn.disabled = false;
            btn.textContent = 'Change IP';
        }
    }
}

refresh();
setInterval(refresh, 5000);