#!/bin/sh

set -eu

usage() {
	cat <<'EOF'
Install fx-openai as a user binary and generate its local systemd service files.

Usage: ./install.sh [-listen ADDR] [-upstream URL] [-api-key KEY]

Environment:
  PREFIX          binary prefix (default: ~/.local)
  XDG_DATA_HOME   data directory (default: ~/.local/share)
  LISTEN          loopback address (default: 127.0.0.1:8787)
  OPENAI_BASE_URL OpenAI-compatible /v1 base URL
  OPENAI_API_KEY  upstream API key; OLLAMA_API_KEY is also accepted
EOF
}

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
prefix=${PREFIX:-"$HOME/.local"}
data_home=${XDG_DATA_HOME:-"$HOME/.local/share"}
share_dir=$data_home/fx-openai
bin_dir=$prefix/bin
listen=${LISTEN:-127.0.0.1:8787}
upstream=${OPENAI_BASE_URL:-https://ollama.com/v1}
api_key=${OPENAI_API_KEY:-${OLLAMA_API_KEY:-}}
model=${FX_MODEL:-}

toml_quote() {
	value=$(printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g')
	printf '"%s"' "$value"
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		-listen)
			[ "$#" -ge 2 ] || { echo "install.sh: -listen needs an address" >&2; exit 2; }
			listen=$2
			shift 2
			;;
		-upstream)
			[ "$#" -ge 2 ] || { echo "install.sh: -upstream needs a URL" >&2; exit 2; }
			upstream=$2
			shift 2
			;;
		-api-key)
			[ "$#" -ge 2 ] || { echo "install.sh: -api-key needs a value" >&2; exit 2; }
			api_key=$2
			shift 2
			;;
		-h|-help|--help)
			usage
			exit 0
			;;
		*)
			echo "install.sh: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
done

# Re-running the installer without a value keeps the existing local settings.
if [ "$upstream" = "https://ollama.com/v1" ] && [ -s "$share_dir/baseURL" ] && [ -z "${OPENAI_BASE_URL:-}" ]; then
	upstream=$(sed -n '1p' "$share_dir/baseURL")
fi
if [ -z "$api_key" ] && [ -f "$share_dir/apikey" ]; then
	api_key=$(sed -n '1p' "$share_dir/apikey")
fi

case "$listen" in
	127.0.0.1:*|localhost:*|\[::1\]:*) ;;
	*)
		echo "install.sh: listen address must be loopback (127.0.0.1, localhost, or [::1])" >&2
		exit 2
		;;
esac

cd "$script_dir"
mkdir -p "$bin_dir" "$share_dir"
go build -o "$bin_dir/fx-openai" ./cmd/fx-openai
chmod 755 "$bin_dir/fx-openai"

if [ ! -f "$share_dir/config.toml" ]; then
	printf '%s\n' \
		"# fx-openai TOML configuration" \
		"listen = $(toml_quote "$listen")" \
		"base_url = $(toml_quote "$upstream")" \
		"api_key = $(toml_quote "plain:$api_key")" \
		"model = $(toml_quote "$model")" >"$share_dir/config.toml"
	chmod 600 "$share_dir/config.toml"
fi

if ! "$bin_dir/fx-openai" service init -config "$share_dir/config.toml"; then
	echo "warning: service unit was generated but systemctl --user is not available; run 'fx-openai service init' later" >&2
fi
unit_dir=${XDG_CONFIG_HOME:-"$HOME/.config"}/systemd/user
if [ -f "$unit_dir/fx-openai.service" ]; then
	cp "$unit_dir/fx-openai.service" "$share_dir/fx-openai.service"
fi
chmod 644 "$share_dir/fx-openai.service" 2>/dev/null || true
rm -f "$share_dir/baseURL" "$share_dir/apikey" "$share_dir/config"

cat >"$share_dir/install-service" <<EOF
#!/bin/sh

set -eu

exec '$bin_dir/fx-openai' -config '$share_dir/config.toml' service "\$@"
EOF
chmod 700 "$share_dir/install-service"

echo "installed $bin_dir/fx-openai"
echo "generated $share_dir"
echo "start service: $share_dir/install-service start"
