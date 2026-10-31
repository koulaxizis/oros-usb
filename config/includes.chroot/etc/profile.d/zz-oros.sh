# Autologin on tty2 starts the network setup. orOS itself runs on tty1 as
# oros-session.service.
if [ "$(id -un)" = "oros" ] && [ -z "${WAYLAND_DISPLAY:-}" ] && [ "$(tty)" = /dev/tty2 ]; then
	exec /usr/local/bin/oros-network
fi
