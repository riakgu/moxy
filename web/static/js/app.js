const API_BASE = '';

function formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(1024));
    return (bytes / Math.pow(1024, i)).toFixed(1) + ' ' + units[i];
}

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
                <td>${formatBytes(slot.bytes_sent)} ↑ / ${formatBytes(slot.bytes_received)} ↓</td>
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
    fetchUsers();
    fetchDestinations();
}

function setStatus(msg, isError) {
    const el = document.getElementById('provision-status');
    el.textContent = msg;
    el.style.color = isError ? '#f85149' : '#58a6ff';
}

function setUserStatus(msg, isError) {
    const el = document.getElementById('user-status');
    el.textContent = msg;
    el.style.color = isError ? '#f85149' : '#58a6ff';
}

// ——— Destinations ———

async function fetchDestinations() {
    try {
        const res = await fetch(`${API_BASE}/api/destinations?limit=50`);
        const json = await res.json();
        const data = json.data || {};
        const dests = data.destinations || [];

        const tbody = document.getElementById('destinations-body');
        tbody.innerHTML = '';

        for (const d of dests) {
            const tr = document.createElement('tr');
            tr.innerHTML = `
                <td>${d.domain}</td>
                <td>${d.connections}</td>
                <td>${formatBytes(d.bytes_sent)} ↑ / ${formatBytes(d.bytes_received)} ↓</td>
                <td>${d.last_accessed ? new Date(d.last_accessed * 1000).toLocaleTimeString() : '-'}</td>
            `;
            tbody.appendChild(tr);
        }
    } catch (err) {
        console.error('Failed to fetch destinations:', err);
    }
}

// ——— Users ———

let editingUser = null;

async function fetchUsers() {
    try {
        const res = await fetch(`${API_BASE}/api/users`);
        const json = await res.json();
        const users = json.data || [];

        const tbody = document.getElementById('users-body');
        tbody.innerHTML = '';

        users.sort((a, b) => a.username.localeCompare(b.username));

        for (const user of users) {
            const tr = document.createElement('tr');
            tr.innerHTML = `
                <td>${user.username}</td>
                <td>${user.device_binding || '—'}</td>
                <td><span class="badge badge-${user.enabled ? 'enabled' : 'disabled'}">${user.enabled ? 'enabled' : 'disabled'}</span></td>
                <td class="actions-cell">
                    <button class="btn btn-sm btn-edit" onclick="editUser('${user.username}')">Edit</button>
                    <button class="btn btn-sm btn-changeip" onclick="toggleUser('${user.username}', ${!user.enabled})">${user.enabled ? 'Disable' : 'Enable'}</button>
                    <button class="btn btn-sm btn-delete" onclick="deleteUser('${user.username}')">Delete</button>
                </td>
            `;
            tbody.appendChild(tr);
        }
    } catch (err) {
        console.error('Failed to fetch users:', err);
    }
}

function showAddUser() {
    editingUser = null;
    document.getElementById('user-username').value = '';
    document.getElementById('user-password').value = '';
    document.getElementById('user-device').value = '';
    document.getElementById('user-username').disabled = false;
    document.getElementById('btn-save-user').textContent = 'Create';
    document.getElementById('user-form').style.display = 'block';
    setUserStatus('', false);
}

async function editUser(username) {
    try {
        const res = await fetch(`${API_BASE}/api/users/${username}`);
        const json = await res.json();
        const user = json.data;

        editingUser = username;
        document.getElementById('user-username').value = user.username;
        document.getElementById('user-username').disabled = true;
        document.getElementById('user-password').value = '';
        document.getElementById('user-device').value = user.device_binding || '';
        document.getElementById('btn-save-user').textContent = 'Update';
        document.getElementById('user-form').style.display = 'block';
        setUserStatus('Leave password empty to keep current', false);
    } catch (err) {
        alert(`Failed to load user: ${err.message}`);
    }
}

function hideUserForm() {
    document.getElementById('user-form').style.display = 'none';
    editingUser = null;
}

async function saveUser() {
    const username = document.getElementById('user-username').value.trim();
    const password = document.getElementById('user-password').value;
    const device = document.getElementById('user-device').value.trim();

    if (!username) {
        setUserStatus('Username is required', true);
        return;
    }

    const btn = document.getElementById('btn-save-user');
    btn.disabled = true;

    try {
        if (editingUser) {
            const body = { device_binding: device };
            if (password) body.password = password;
            const res = await fetch(`${API_BASE}/api/users/${editingUser}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body)
            });
            if (!res.ok) {
                const json = await res.json();
                setUserStatus(`Update failed: ${json.errors || res.statusText}`, true);
                return;
            }
            setUserStatus('User updated', false);
        } else {
            if (!password) {
                setUserStatus('Password is required for new users', true);
                return;
            }
            const res = await fetch(`${API_BASE}/api/users`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ username, password, device_binding: device, enabled: true })
            });
            if (!res.ok) {
                const json = await res.json();
                setUserStatus(`Create failed: ${json.errors || res.statusText}`, true);
                return;
            }
            setUserStatus('User created', false);
        }

        hideUserForm();
        fetchUsers();
    } catch (err) {
        setUserStatus(`Error: ${err.message}`, true);
    } finally {
        btn.disabled = false;
    }
}

async function toggleUser(username, enabled) {
    try {
        const res = await fetch(`${API_BASE}/api/users/${username}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ enabled })
        });
        if (!res.ok) {
            const json = await res.json();
            alert(`Toggle failed: ${json.errors || res.statusText}`);
            return;
        }
        fetchUsers();
    } catch (err) {
        alert(`Toggle error: ${err.message}`);
    }
}

async function deleteUser(username) {
    if (!confirm(`Delete user "${username}"?`)) return;

    try {
        const res = await fetch(`${API_BASE}/api/users/${username}`, { method: 'DELETE' });
        if (!res.ok) {
            const json = await res.json();
            alert(`Delete failed: ${json.errors || res.statusText}`);
            return;
        }
        fetchUsers();
    } catch (err) {
        alert(`Delete error: ${err.message}`);
    }
}

// ——— Slots ———

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