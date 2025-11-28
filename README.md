
***

# BTRFS Web Manager

A lightweight, single-binary web interface and scheduler for managing BTRFS filesystems. This tool provides a dashboard to monitor, schedule, and execute BTRFS maintenance tasks such as snapshots, scrubs, balancing, and defragmentation.

It is designed to run as a Docker container or a standalone binary on Linux systems.

## Features

*   **Snapshot Management**
    *   Take immediate snapshots.
    *   Schedule snapshots using intervals (e.g., every 15 minutes) or Cron expressions.
    *   Automated Retention Policy: cleanup old snapshots based on count (e.g., keep last 10) or time (e.g., keep last 7 days).
*   **Filesystem Maintenance**
    *   **Scrub:** Schedule and trigger filesystem scrubs to verify data integrity.
    *   **Balance:** Schedule and trigger balancing to reclaim unallocated space.
    *   **Defragmentation:** Trigger recursive defragmentation on specific paths.
    *   **Compression Analysis:** Run `compsize` to view compression savings and ratios.
*   **Activity Logging**
    *   Persistent history of all operations with success/failure status and full command output.
    *   Real-time status updates for long-running operations.
*   **User Interface**
    *   Responsive, mobile-friendly web dashboard.
    *   Dark/Light mode toggle (persisted in local storage).
    *   No external dependencies (HTML/CSS embedded in binary).

## Installation

### Method 1: Docker (Recommended)

Docker is the easiest way to run the manager. The container requires privileged mode to execute BTRFS commands on the host filesystem.

1.  Create a `docker-compose.yml` file:

```yaml
services:
  btrfs-manager:
    image: ghcr.io/yourusername/btrfs-manager:latest # Or build locally
    container_name: btrfs_web_ui
    privileged: true
    network_mode: bridge
    environment:
      - TZ=UTC  # Set your local timezone (e.g., America/New_York)
    volumes:
      # Map the host root directory so the container can access drives
      - /:/host
      # Persist application logs and configuration
      - ./data:/data
    ports:
      - "8080:8080"
    restart: unless-stopped
```

2.  Start the container:
    ```bash
    docker compose up -d
    ```

3.  Access the web interface at `http://localhost:8080`.

**Note on Paths:** Because the host root is mounted to `/host` inside the container, you must configure your paths in the Web UI to start with `/host`.
*   Example Host Path: `/mnt/data`
*   Web UI Path: `/host/mnt/data`

### Method 2: Standalone Binary

You can run the binary directly on your host machine without Docker.

**Prerequisites:**
*   Linux OS
*   `btrfs-progs` installed (for btrfs commands).
*   `compsize` installed (optional, for compression analysis).

**Installation:**
1.  Download the latest release for your architecture (AMD64 or ARM64) from the Releases page.
2.  Make the binary executable:
    ```bash
    chmod +x btrfs-manager-linux-amd64
    ```
3.  Run the binary (Root privileges are usually required for BTRFS commands):
    ```bash
    sudo ./btrfs-manager-linux-amd64
    ```

## Configuration

Once the application is running, open the Web UI to configure the settings.

### Global Settings
*   **Target Drive:** The mount point to perform Scrub, Balance, Defrag, and Compression checks on (e.g., `/host/mnt/disk1`).
*   **Snapshot Source:** The subvolume or directory you want to backup (e.g., `/host/home`).
*   **Snapshot Destination:** Where the read-only snapshots will be stored (e.g., `/host/home/.snapshots`).

### Scheduling
You can configure independent schedules for Snapshots, Scrub, and Balance.
*   **Every X:** Runs the task at a fixed interval (Minutes, Hours, or Days).
*   **Cron:** Use standard Cron syntax (e.g., `*/15 * * * *` for every 15 minutes).

### Retention Policy
Automatically delete old snapshots to save space.
*   **Keep Last (Count):** Retains a specific number of the most recent snapshots.
*   **Keep Older Than (Time):** Retains snapshots only for a specific duration (Days, Weeks, Months, Years).

## Development

To build this project from source, you need Go 1.25+.

1.  Clone the repository:
    ```bash
    git clone https://github.com/yourusername/btrfs-manager.git
    cd btrfs-manager
    ```

2.  Build the binary:
    ```bash
    CGO_ENABLED=0 GOOS=linux go build -o btrfs-manager .
    ```

## Troubleshooting

**"Read-only file system" error**
Ensure the container has the `privileged: true` flag in Docker Compose and that the destination drive is mounted with write permissions.

**Path not found**
If running in Docker, remember that the application sees the filesystem from inside the container. If you mapped `/` on the host to `/host` in the container, all your paths in the Web UI config must be prefixed with `/host`.

**Timezone is incorrect**
Ensure the `TZ` environment variable is set correctly in your Docker config (e.g., `TZ=Europe/London`). Timestamps in filenames and logs rely on this setting.

