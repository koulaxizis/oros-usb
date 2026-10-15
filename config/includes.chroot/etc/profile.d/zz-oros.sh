# Autologin on tty1 starts orOS; on tty2 the network setup.
if [ "$(id -un)" = "oros" ] && [ -z "${WAYLAND_DISPLAY:-}" ]; then
	case "$(tty)" in
	/dev/tty1) exec /usr/local/bin/oros-session ;;
	/dev/tty2) exec /usr/local/bin/oros-network ;;
	esac
fi
