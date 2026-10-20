#!/usr/bin/env bash
# Linux setup for f108-pro: hidraw permissions, F-keys over the dongle, the
# binary, and the clock sync service. Safe to re-run. See README "Linux".
set -euo pipefail
cd "$(dirname "$0")/.."

echo "1/4 udev rule for the keyboard and the dongle (sudo)"
sudo tee /etc/udev/rules.d/70-aula-f108pro.rules >/dev/null <<'EOF'
SUBSYSTEM=="hidraw", ATTRS{idVendor}=="0c45", ATTRS{idProduct}=="800a", TAG+="uaccess"
SUBSYSTEM=="hidraw", ATTRS{idVendor}=="05ac", ATTRS{idProduct}=="024f", TAG+="uaccess"
EOF
sudo udevadm control --reload-rules
sudo udevadm trigger --subsystem-match=hidraw

echo "2/4 hid_apple fnmode=0, so F1-F12 are F-keys over the dongle (sudo)"
echo "options hid_apple fnmode=0" | sudo tee /etc/modprobe.d/hid_apple.conf >/dev/null
if [ -e /sys/module/hid_apple/parameters/fnmode ]; then # Apply now, without a reboot.
	echo 0 | sudo tee /sys/module/hid_apple/parameters/fnmode >/dev/null
fi
if command -v dracut >/dev/null; then
	sudo dracut -f
elif command -v update-initramfs >/dev/null; then
	sudo update-initramfs -u
fi

echo "3/4 build ~/.local/bin/f108-pro"
mkdir -p "$HOME/.local/bin"
go build -o "$HOME/.local/bin/f108-pro" .

echo "4/4 systemd user service f108-pro-watch"
install -Dm644 contrib/f108-pro-watch.service "$HOME/.config/systemd/user/f108-pro-watch.service"
systemctl --user daemon-reload
systemctl --user enable f108-pro-watch
systemctl --user restart f108-pro-watch

echo "Done. fnmode=$(cat /sys/module/hid_apple/parameters/fnmode 2>/dev/null || echo 'not loaded')"
echo "Service log: journalctl --user -u f108-pro-watch -f"
