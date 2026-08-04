# BTRFS Commander

A lightweight, powerful web interface and scheduler for managing BTRFS filesystems. Monitor drive health, automate snapshots, and manage filesystem maintenance through a modern, responsive dashboard.

## 🚀 Key Features

### 📸 Advanced Snapshot Management
*   **Multiple Backup Jobs:** Define multiple source/destination pairs with independent configurations.
*   **Flexible Scheduling:** Use simple intervals (e.g., every 30 mins) or full Cron expressions.
*   **Smart Retention:** Automatically clean up old snapshots based on count or time (Days/Weeks/Months/Years).
*   **Immutable (Locked) Snapshots:** Click the 🔒 button to lock specific snapshots. Locked snapshots are protected from the retention policy and will be kept forever, regardless of your age or count limits. By default, all snapshots are eligible for deletion by retention policies unless you explicitly lock them.
*   **Rollback & Recovery:** Instantly restore live data from any snapshot with automatic safety backups.
*   **Snapshot Explorer:** Browse and download individual files directly from your snapshots without mounting them manually.
*   **Visual Diff:** Compare two snapshots to see changed files (using `btrfs send` logic).

### 🛠 Filesystem Maintenance
*   **Scrub & Balance:** Schedule or manually trigger scrubs and balances to ensure data integrity and reclaim space.
*   **Defragmentation:** Run recursive defragmentation on target drives.
*   **Compression Analysis:** Integrated `compsize` support to view real-world compression ratios and savings.

### 🩺 Health & Monitoring
*   **SMART Integration:** Monitor drive health and trigger Short/Long self-tests directly from the UI.
*   **Error Tracking:** Real-time monitoring of BTRFS device stats (read/write/checksum errors).
*   **Activity Calendar:** A full calendar view and dashboard mini-widget to track snapshot history and maintenance events.
*   **Missed Job Alerts:** Visual indicators (orange/yellow) highlight when scheduled tasks were missed.

### ⚙️ Automation & Migration
*   **Hooks:** Execute custom shell commands before or after snapshot jobs.
*   **Replication:** Automated BTRFS replication via `send/receive` to secondary backup targets.
*   **Config Import/Export:** Backup your entire setup (Jobs, Schedules, Settings) to a JSON file for easy recovery or migration.
*   **Secure Access:** Optional password protection with session-based authentication.
*   **Verbose Logging:** Set log level to VERBOSE for detailed debugging output of all operations.

---

## 📦 Installation

### Method 1: Docker (Recommended)
The container requires `privileged` mode to execute BTRFS and SMART commands on the host.

```yaml
services:
  btrfs-commander:
    image: ghcr.io/hkcfs/btrfs-commander:latest
    container_name: btrfs_commander
    privileged: true
    environment:
      - TZ=UTC
      - PORT=8080
      - PASSWORD=your_secure_password # Optional
    volumes:
      - /:/host           # Map host root to access drives
      - ./data:/data      # Persist config and logs
    ports:
      - "8080:8080"
    restart: unless-stopped
```

### Method 2: Standalone Binary
1. Download the latest release for your architecture.
2. Ensure `btrfs-progs`, `smartmontools`, and `compsize` are installed on your host.
3. Run with sudo: `sudo ./btrfs-commander`

---

## 🔧 Configuration

### 1. Global Settings
- **Target Drive:** Set the mount point for maintenance tasks (e.g., `/host/mnt/data`).
- **Log Level:** Choose between Default, Verbose (for debugging), or None.

### 2. Job Configuration
Each Backup Job includes:
- **Source/Destination:** Full paths (prefixed with `/host` if using Docker).
- **Scheduling:** Interval or Cron.
- **Retention:** Define how many/how long to keep snapshots.
- **Hooks:** Script triggers for integration with other tools.
- **Replication:** Target path for `btrfs send`.

### 3. Locked Snapshots
- Click the 🔒 icon next to any snapshot to protect it from retention policies
- Locked snapshots will never be deleted automatically
- Click 🔓 to unlock and allow deletion again
- Lock status is preserved across restarts

### 4. Config Backup/Restore
- **Export:** Download your complete configuration as JSON from the Settings page
- **Import:** Upload a previously exported JSON file to restore all jobs and settings
- Useful for migrating to a new server or disaster recovery

---

## 🛠 Development
Requires Go 1.22+ and Node.js for frontend assets (if applicable).

1. Clone the repo: `git clone https://github.com/hkcfs/btrfs-webui.git`
2. Build: `CGO_ENABLED=0 go build -o btrfs-commander ./cmd/server`

---

## 🔐 Security

This tool executes `btrfs`, `smartctl`, and other commands against paths you
configure. In Docker it runs **privileged with the host root mounted at
`/host`**, so it is root-equivalent on the host machine. Treat it as a
management console for root access, not as a low-privilege service.

- **Always set `PASSWORD`** to a strong, unique value. Without it, every
  endpoint is open to anyone who can reach the port.
- The old default `PASSWORD=admin` from the sample `docker-compose.yml` has
  been removed; never use it.
- Only expose the port on a **trusted network**. The built-in auth is
  single-factor HTTP login, not a substitute for a firewall or VPN.
- For extra safety, bind to loopback only and reach the UI through an SSH
  tunnel or a reverse proxy that adds TLS and real authentication:
  `BIND_ADDR=127.0.0.1:8080`.
- Session cookies are `HttpOnly` + `SameSite=Strict`, server-side tokens are
  random, and login attempts are rate limited. The session lasts 24 hours.
- Snapshots can contain symlinks; the file browser only serves paths that
  resolve inside a configured backup destination.

## 📜 License
 GPL-3.0 License. See `LICENSE` for details.
