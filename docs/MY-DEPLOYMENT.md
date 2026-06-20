# Deploying Do-It on Android (Termux) with Static IP

A complete guide to setting up a local-first task graph server on a repurposed Android phone.

---

## Overview

This guide walks you through:
- Cross-compiling the Go binary on your Mac for Android ARM64.
- Transferring and running the binary on your Android phone via Termux.
- Configuring a static IP address so your server is always reachable at the same address.
- Setting up battery optimizations to keep the server running indefinitely.

---

## Prerequisites

**On Your Mac:**
- Go (Golang) installed.
- Git installed.
- The Do-It project cloned locally.

**On Your Android Phone:**
- **Termux** installed from **F-Droid** (NOT Google Play Store - the Play Store version is outdated and broken).
- Phone connected to your home Wi-Fi network.
- A Vivo/Android phone you can dedicate as a server.

---

## Part 1: Cross-Compile the Binary on Mac

Open Terminal on your Mac and navigate to your Do-It project:

```bash
cd /path/to/your/Do-It

# Cross-compile for Android (Linux ARM64)
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o doit-server .
```

**What this does:**
- `GOOS=linux` - Targets Linux (Android is Linux-based).
- `GOARCH=arm64` - Targets ARM 64-bit processors (all modern Android phones).
- `CGO_ENABLED=0` - Creates a static binary with no C library dependencies.
- `-o doit-server` - Names the output file `doit-server`.

You now have a single executable file that contains both your Go backend and embedded frontend.

---

## Part 2: Transfer the Binary to Your Phone

### Step 2.1: Start a Temporary HTTP Server on Mac

In the same directory where you built the binary:

```bash
python3 -m http.server 8000
```

Leave this running. Note your Mac's IP address by opening a new terminal tab:

```bash
ipconfig getifaddr en0
```

You should see something like `192.168.1.15`.

### Step 2.2: Download on Android (Termux)

Open Termux on your Android phone:

```bash
# Navigate to home directory
cd ~

# Download the binary (replace with YOUR Mac's IP)
curl -O http://192.168.1.15:8000/doit-server

# Make it executable
chmod +x doit-server

# Verify it downloaded
ls -lh doit-server
```

You can now stop the Python server on your Mac (`Ctrl+C`).

---

## Part 3: Set Up Data Directory

The server needs a place to store `state.json` and uploaded files.

```bash
# Create the data directory
mkdir -p ~/doit-data

# Verify it exists
ls -ld ~/doit-data
```

**Alternative:** If you want your data visible in your phone's normal file manager:

```bash
# Grant storage permission (run this once)
termux-setup-storage

# Use shared storage instead
mkdir -p ~/storage/shared/DoItData
```

---

## Part 4: Configure Static IP on Android

This ensures your phone always gets the same IP address, so you don't have to hunt for it after reboots.

### Step 4.1: Change to Static IP

On your Android phone:
1. Go to **Settings** → **Wi-Fi** (or **Network & Internet** → **Wi-Fi**).
2. Tap the **gear icon** or **arrow (>)** next to your connected network.
3. Tap **Advanced** or **IP Settings**.
4. Change from **DHCP** to **Static**.

### Step 4.2: Fill in the Details

Enter exactly these values:

- **IP Address:** `192.168.1.50` *(Choose an IP outside your router's DHCP pool, usually `.100` to `.254`)*
- **Gateway:** `192.168.1.1` *(Must match your router's IP)*
- **Network Prefix Length:** `24` *(or Subnet Mask: `255.255.255.0`)*
- **DNS 1:** `192.168.1.1`
- **DNS 2:** `8.8.8.8` *(Optional)*

Tap **Save** or **Connect**. Your phone will disconnect and reconnect with the new static IP.

---

## Part 5: Configure Battery Optimizations (Critical!)

Android aggressively kills background apps. You MUST disable battery optimization for Termux.

### On Vivo / Funtouch OS:

1. **Settings** → **Battery** → **Background power consumption** (or High background power consumption).
2. Find **Termux** in the list and set to **Allow** or **No restrictions**.
3. **Settings** → **Apps** → **Termux** → **Battery** → Set to **Unrestricted** or **Don't optimize**.

### Lock the App in Memory:

1. Open recent apps (swipe up and hold from the bottom).
2. Find Termux.
3. Tap the three dots or menu icon above the app.
4. Select **Lock** or tap the padlock icon. This prevents Android from killing Termux when you "clear all" apps.

---

## Part 6: Run the Server

### First Time Start:

In Termux:

```bash
# If using ~/doit-data
DOIT_ADDR=0.0.0.0:8080 DOIT_DATA_DIR=~/doit-data ./doit-server

# OR if using shared storage
DOIT_ADDR=0.0.0.0:8080 DOIT_DATA_DIR=~/storage/shared/DoItData ./doit-server
```

**What this does:**
- `DOIT_ADDR=0.0.0.0:8080` - Listens on all network interfaces (not just localhost).
- `DOIT_DATA_DIR=...` - Specifies where to save tasks and uploads.
- `./doit-server` - Runs the binary.

You should see output like:
```text
Starting server on 0.0.0.0:8080
Listening on port 8080...
```

**Leave Termux open** for the server to stay running.

---

## Part 7: Test from Your Laptop

1. Make sure your laptop is on the **same Wi-Fi network** as your phone.
2. Open a browser on your laptop.
3. Go to: `http://192.168.1.50:8080`

You should see the Do-It interface! Create a task on your laptop and verify it appears in the Termux logs without errors.

---

## Part 8: Keep It Running (Advanced)

### Create a Simple Run Script

To make it easier to start the server:

```bash
nano ~/run-doit.sh
```

Paste this:

```bash
#!/data/data/com.termux/files/usr/bin/bash

# Acquire wake lock (prevents CPU sleep)
termux-wake-lock

# Set environment
export DOIT_ADDR="0.0.0.0:8080"
export DOIT_DATA_DIR="$HOME/doit-data"

# Run server
cd ~
./doit-server
```

Save (`Ctrl+O`, `Enter`) and exit (`Ctrl+X`). Make it executable:

```bash
chmod +x ~/run-doit.sh
```

Now you can just run: `~/run-doit.sh`

### Optional: Auto-Start on Boot

If you want the server to start automatically when the phone boots:

1. Install **Termux:Boot** from **F-Droid** (must match your Termux source!).
2. Open the Termux:Boot app once to register it.
3. Create the boot directory: `mkdir -p ~/.termux/boot`
4. Copy your run script: `cp ~/run-doit.sh ~/.termux/boot/`
5. Reboot your phone to test.

---

## Troubleshooting

- **"Connection refused" from laptop:** Verify phone and laptop are on the same Wi-Fi. Ensure you are using `http://192.168.1.50:8080` (not `localhost`).
- **"Permission denied" errors:** Make sure you ran `chmod +x doit-server`.
- **Server keeps stopping:** Disable battery optimization (Part 5) and keep Termux locked in recent apps.
- **Tasks not saving:** Check Termux terminal for error messages. Verify `DOIT_DATA_DIR` points to an existing directory.