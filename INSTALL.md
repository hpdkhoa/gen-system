# gen-system installation kit

Three scripts for installing, registering as a service, and removing
gen-system on a fresh Linux server.

## What gets installed

| Component       | Version            | Where                          |
|-----------------|--------------------|--------------------------------|
| Go              | 1.25.0             | `/usr/local/go`                |
| Ollama          | latest             | `/usr/local/bin/ollama` (per upstream installer) |
| qwen3:14b, qwen2.5-coder:14b | latest | Ollama models dir              |
| PostgreSQL      | distro-default 15+ | system package paths           |
| gen, gen-server | from your source   | `/opt/gen-system/bin`          |
| web/ assets     | from your source   | `/opt/gen-system/web`          |
| service user    | gensystem          | `/var/lib/gen-system`          |
| systemd unit    | gen-server.service | `/etc/systemd/system`          |
| environment     | gen-system env     | `/etc/gen-system/environment`  |

Tested on Ubuntu 22.04, Ubuntu 24.04, Debian 12. Falls back to dnf/yum on
RHEL-family distros. AMD64 and ARM64 supported.

## Prerequisites

- A fresh Linux server with sudo/root access
- Disk for the Go toolchain, the two Ollama models and your data
- Internet access at install time (afterwards everything runs offline)
- Your gen-system source tree (the full project, not just the patches)

## Quick start

```bash
# 1. Place these scripts at the root of the gen-system source tree
#    (or one level above), next to go.mod.
ls
# → install.sh  install-service.sh  uninstall.sh  go.mod  generator/ ...

# 2. Make them executable
chmod +x install.sh install-service.sh uninstall.sh

# 3. Install everything
sudo ./install.sh

# 4. Register as systemd service that auto-starts on boot
sudo ./install-service.sh

# 5. Open the studio (URL is printed at the end)
firefox http://<server-ip>:28080/intelligence.html
```

## Common variations

```bash
# Skip Postgres (using your own DB elsewhere)
sudo ./install.sh --no-postgres

# Skip Ollama (already installed, or running on another machine)
sudo ./install.sh --no-ollama

# Skip the model pulls (do them later with `ollama pull qwen3:14b` and `ollama pull qwen2.5-coder:14b`)
sudo ./install.sh --skip-model

# Install to a non-default prefix
sudo ./install.sh --prefix=/srv/gen-system

# Service on a different port, custom project root
sudo ./install-service.sh --port=9090 --root=/srv/myprojects

# Service with COBOL copybook resolution enabled
sudo ./install-service.sh --copybook-path=/srv/copybooks

# Service with GitHub routes enabled (mounts /api/github/*)
sudo ./install-service.sh --github-enabled
```

## Managing the service

```bash
# Status, logs, control
systemctl status   gen-server
systemctl restart  gen-server
systemctl stop     gen-server
journalctl -u gen-server -f          # follow logs

# Disable auto-start on boot (but keep it installed)
sudo systemctl disable gen-server

# Re-enable
sudo systemctl enable gen-server
```

## Changing settings

Edit `/etc/gen-system/environment` and restart:

```bash
sudo nano /etc/gen-system/environment
sudo systemctl restart gen-server
```

Variables you can change:

```
GEN_PORT=28080
GEN_ROOT=/var/lib/gen-system/projects
GEN_COBOL_COPYBOOK_PATH=/var/lib/gen-system/copybooks
OLLAMA_HOST=http://127.0.0.1:11434
GEN_SERVER_EXTRA_FLAGS=
GEN_DB_URL=postgres://gensystem:...@localhost:5432/gensystem?sslmode=disable
```

## File system layout after install

```
/opt/gen-system/
  bin/
    gen
    gen-server
  web/
    intelligence.html
    ...
  etc/
    database.env       (if Postgres was installed)

/var/lib/gen-system/   (owned by gensystem)
  .gen/                gen-system's internal state cache
  projects/            put your codebases here
  copybooks/           COBOL copybooks (if you use them)

/var/log/gen-system/   (owned by gensystem)

/etc/gen-system/
  environment          service configuration

/etc/systemd/system/
  gen-server.service   systemd unit file
```

## Uninstall

```bash
# Remove the service and binaries, keep the data
sudo ./uninstall.sh

# Remove everything including data, postgres DB, and the model
sudo ./uninstall.sh --purge

# Remove everything but leave postgres and ollama alone
sudo ./uninstall.sh --purge --keep-postgres --keep-ollama
```

## Troubleshooting

**The studio is unreachable from another machine.**
Open the firewall on the configured port:
```bash
sudo ufw allow 28080/tcp        # Ubuntu/Debian
sudo firewall-cmd --add-port=28080/tcp --permanent && sudo firewall-cmd --reload  # RHEL
```

**`go: command not found` after install.**
Either log out and back in, or source the profile:
```bash
. /etc/profile.d/go.sh
```

**`ollama: connection refused`.**
The Ollama systemd service may not have started. Check and start it:
```bash
sudo systemctl status ollama
sudo systemctl start ollama
```

**`gen-server` fails to start with "permission denied" on `/var/lib/gen-system`.**
The service user lost ownership of its directories. Restore:
```bash
sudo chown -R gensystem:gensystem /var/lib/gen-system /var/log/gen-system
sudo systemctl restart gen-server
```

**Out of memory on a small VPS.**
The systemd unit caps memory at 4 GB. Edit `/etc/systemd/system/gen-server.service`,
change `MemoryMax=4G`, reload:
```bash
sudo systemctl daemon-reload
sudo systemctl restart gen-server
```

**Ollama is running but generation is slow.**
The models need GPU acceleration for reasonable speed on real workloads.
Without a GPU, the studio still parses code and shows intelligence. Only the
generation features will be slow. For NVIDIA GPUs, install CUDA drivers before
running `ollama pull` and Ollama will detect and use them automatically.

## Going offline

Once installed, gen-system runs fully offline. The only network operations are:
- Initial download of Go and Ollama installers
- `ollama pull qwen3:14b` and `ollama pull qwen2.5-coder:14b` (once)

After install you can disconnect the server from the internet entirely. The
engine will keep working. To re-pull or update the model, you'll need network
access again.
